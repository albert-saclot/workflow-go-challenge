package workflow

import (
	"context"
	"fmt"
	"time"

	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
)

const (
	// maxExecutionSteps is a safeguard against malformed workflows.
	maxExecutionSteps = 100

	// nodeTimeout limits how long a single node can execute.
	// Prevents slow external API calls from blocking the entire workflow.
	nodeTimeout = 10 * time.Second

	// workflowTimeout bounds the total execution time across all nodes.
	// Without this, a long chain of nodes could block the HTTP handler indefinitely.
	workflowTimeout = 60 * time.Second
)

// StepResult captures the outcome of executing a single node.
type StepResult struct {
	NodeID      string         `json:"nodeId"`
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Status      string         `json:"status"`
	DurationMs  int64          `json:"durationMs"`
	Output      map[string]any `json:"output,omitempty"`
	Error       string         `json:"error,omitempty"`
}

// ExecutionResponse is the JSON response for the execute endpoint.
// On failure, Status is "failed", FailedNode identifies which node
// broke, and Steps contains partial results up to and including the
// failed node.
type ExecutionResponse struct {
	ExecutedAt string       `json:"executedAt"`
	Status     string       `json:"status"`
	Steps      []StepResult `json:"steps"`
	FailedNode string       `json:"failedNode,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// edgeTarget represents a single outgoing edge from a node.
// sourceHandle is non-nil for condition branches ("true"/"false").
type edgeTarget struct {
	TargetID     string
	SourceHandle *string
}

// executeWorkflow walks the workflow graph from the start node, executing
// each node in sequence and following edges (including condition branches).
// Returns partial results on failure so the caller can show which node broke.
func executeWorkflow(ctx context.Context, wf *storage.Workflow, inputs map[string]any, deps nodes.Deps) (*ExecutionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, workflowTimeout)
	defer cancel()

	// 1. Construct typed nodes from storage data
	nodeMap := make(map[string]nodes.Node)
	nodeInfo := make(map[string]storage.Node) // keep storage info for step results

	for _, sn := range wf.Nodes {
		base := nodes.BaseFields{
			ID:          sn.ID,
			NodeType:    sn.Type,
			Position:    nodes.Position{X: sn.Position.X, Y: sn.Position.Y},
			Label:       sn.Data.Label,
			Description: sn.Data.Description,
			Metadata:    sn.Data.Metadata,
		}

		n, err := nodes.New(base, deps)
		if err != nil {
			return nil, fmt.Errorf("failed to construct node %q: %w", sn.ID, err)
		}
		nodeMap[sn.ID] = n
		nodeInfo[sn.ID] = sn
	}

	// 2. Build adjacency list from edges.
	// Key: sourceID → list of edges (for condition branching, multiple edges per source)
	adjacency := make(map[string][]edgeTarget)
	for _, e := range wf.Edges {
		adjacency[e.Source] = append(adjacency[e.Source], edgeTarget{
			TargetID:     e.Target,
			SourceHandle: e.SourceHandle,
		})
	}

	// 3. Validate the graph structure before executing any nodes.
	// This catches cycles and missing start nodes upfront, avoiding
	// wasted API calls on malformed workflows.
	startID, err := validateDAG(wf.Nodes, adjacency)
	if err != nil {
		return nil, err
	}

	// 4. Walk the graph, executing each node
	nCtx := &nodes.NodeContext{Variables: make(map[string]any)}
	for k, v := range inputs {
		nCtx.Variables[k] = v
	}

	var steps []StepResult
	currentID := startID

	for currentID != "" {
		// Check if the request context has been cancelled (client disconnect, timeout)
		if err := ctx.Err(); err != nil {
			return &ExecutionResponse{
				Status:     "cancelled",
				Steps:      steps,
				FailedNode: currentID,
				Error:      fmt.Sprintf("execution cancelled: %s", err.Error()),
			}, nil
		}

		// Guard against runaway workflows
		if len(steps) >= maxExecutionSteps {
			return &ExecutionResponse{
				Status:     "failed",
				Steps:      steps,
				FailedNode: currentID,
				Error:      "workflow exceeded maximum execution steps",
			}, nil
		}

		node, ok := nodeMap[currentID]
		if !ok {
			return &ExecutionResponse{
				Status:     "failed",
				Steps:      steps,
				FailedNode: currentID,
				Error:      fmt.Sprintf("node %q not found in workflow", currentID),
			}, nil
		}
		info := nodeInfo[currentID]

		start := time.Now()
		nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
		result, err := node.Execute(nodeCtx, nCtx)
		cancel()
		elapsed := time.Since(start).Milliseconds()

		if err != nil {
			// Append the failed step with error details, then return partial results
			steps = append(steps, StepResult{
				NodeID:      info.ID,
				Type:        info.Type,
				Label:       info.Data.Label,
				Description: info.Data.Description,
				Status:      "error",
				DurationMs:  elapsed,
				Error:       err.Error(),
			})
			return &ExecutionResponse{
				Status:     "failed",
				Steps:      steps,
				FailedNode: info.ID,
				Error:      fmt.Sprintf("node %q failed: %s", info.ID, err.Error()),
			}, nil
		}

		// Merge output variables into context for downstream nodes
		for k, v := range result.Output {
			nCtx.Variables[k] = v
		}

		steps = append(steps, StepResult{
			NodeID:      info.ID,
			Type:        info.Type,
			Label:       info.Data.Label,
			Description: info.Data.Description,
			Status:      result.Status,
			DurationMs:  elapsed,
			Output:      result.Output,
		})

		// 5. Follow the correct outgoing edge
		currentID = nextNode(adjacency[currentID], result.Branch)
	}

	return &ExecutionResponse{
		Status: "completed",
		Steps:  steps,
	}, nil
}

// validateDAG checks that the workflow graph is a valid DAG before execution.
// Returns the start node ID, or an error if the graph has no start node or
// contains cycles. Uses DFS with three-color marking (unvisited/visiting/done).
func validateDAG(storageNodes []storage.Node, adjacency map[string][]edgeTarget) (string, error) {
	var startID string
	for _, n := range storageNodes {
		if n.Type == "start" {
			startID = n.ID
			break
		}
	}
	if startID == "" {
		return "", fmt.Errorf("workflow has no start node")
	}

	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int)

	var dfs func(string) error
	dfs = func(id string) error {
		state[id] = visiting
		for _, e := range adjacency[id] {
			switch state[e.TargetID] {
			case visiting:
				return fmt.Errorf("cycle detected at node %q", e.TargetID)
			case unvisited:
				if err := dfs(e.TargetID); err != nil {
					return err
				}
			}
		}
		state[id] = done
		return nil
	}

	if err := dfs(startID); err != nil {
		return "", err
	}
	return startID, nil
}

// nextNode picks the next node based on outgoing edges and an optional branch.
// For condition nodes, branch matches the edge's sourceHandle ("true"/"false").
// For regular nodes, follows the single outgoing edge (sourceHandle is nil).
func nextNode(edges []edgeTarget, branch string) string {
	if len(edges) == 0 {
		return "" // end of graph
	}

	// If there's a branch (condition node), match by sourceHandle
	if branch != "" {
		for _, e := range edges {
			if e.SourceHandle != nil && *e.SourceHandle == branch {
				return e.TargetID
			}
		}
		return "" // no matching branch
	}

	// Non-branching node: follow the single outgoing edge
	for _, e := range edges {
		if e.SourceHandle == nil {
			return e.TargetID
		}
	}

	// Fallback: take the first edge
	return edges[0].TargetID
}
