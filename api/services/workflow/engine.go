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
	targetID     string
	sourceHandle *string
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
			targetID:     e.Target,
			sourceHandle: e.SourceHandle,
		})
	}

	// 3. Find the start node
	var startID string
	for _, sn := range wf.Nodes {
		if sn.Type == "start" {
			startID = sn.ID
			break
		}
	}
	if startID == "" {
		return nil, fmt.Errorf("workflow has no start node")
	}

	// 4. Walk the graph, executing each node
	nCtx := &nodes.NodeContext{Variables: make(map[string]any)}
	for k, v := range inputs {
		nCtx.Variables[k] = v
	}

	var steps []StepResult
	visited := make(map[string]bool)
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

		// Detect cycles — a node should never be visited twice
		if visited[currentID] {
			return &ExecutionResponse{
				Status:     "failed",
				Steps:      steps,
				FailedNode: currentID,
				Error:      fmt.Sprintf("cycle detected at node %q", currentID),
			}, nil
		}
		visited[currentID] = true

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

		nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
		result, err := node.Execute(nodeCtx, nCtx)
		cancel()
		if err != nil {
			// Append the failed step with error details, then return partial results
			steps = append(steps, StepResult{
				NodeID:      info.ID,
				Type:        info.Type,
				Label:       info.Data.Label,
				Description: info.Data.Description,
				Status:      "error",
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
			if e.sourceHandle != nil && *e.sourceHandle == branch {
				return e.targetID
			}
		}
		return "" // no matching branch
	}

	// Non-branching node: follow the single outgoing edge
	for _, e := range edges {
		if e.sourceHandle == nil {
			return e.targetID
		}
	}

	// Fallback: take the first edge
	return edges[0].targetID
}
