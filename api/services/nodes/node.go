package nodes

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/pkg/clients/weather"
)

// NodeContext carries runtime variables between nodes during execution.
type NodeContext struct {
	Variables map[string]any
}

// ExecutionResult holds the output of a single node's execution.
// Branch is used by condition nodes to signal which edge to follow
// (matches the sourceHandle on outgoing edges, e.g. "true"/"false").
type ExecutionResult struct {
	Status string         `json:"status"`
	Output map[string]any `json:"output,omitempty"`
	Branch string         `json:"branch,omitempty"`
}

// Position represents a node's canvas coordinates.
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeData holds the display and logic payload for the frontend.
type NodeData struct {
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
}

// NodeJSON is the React Flow representation of a node.
type NodeJSON struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Position Position `json:"position"`
	Data     NodeData `json:"data"`
}

// BaseFields holds the instance-level data that every node type shares.
// These come from the workflow_node_instances + node_library join.
type BaseFields struct {
	ID          string
	NodeType    string
	Position    Position
	Label       string
	Description string
	Metadata    json.RawMessage // raw DB metadata, preserved for ToJSON()
}

// Node is implemented by each node type. A node constructs itself from
// database metadata, can serialize to JSON for the frontend, and can
// execute its own logic during workflow runs.
type Node interface {
	// ToJSON returns the React Flow representation of this node.
	// Metadata is passed through from the DB, not reconstructed.
	ToJSON() NodeJSON
	// Execute runs the node's logic using the shared runtime context.
	Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error)
}

// Deps holds external clients that nodes may need during execution.
// Passed into the factory so nodes stay decoupled from concrete implementations.
type Deps struct {
	Weather weather.Client
	Email   email.Client
}

// New constructs the appropriate node type from its database fields.
// Adding a new node type (e.g., SMS) means adding a case here
// and a new file implementing the Node interface.
func New(base BaseFields, deps Deps) (Node, error) {
	switch base.NodeType {
	case "start", "end":
		return NewPassthroughNode(base), nil
	case "form":
		return NewFormNode(base)
	case "integration":
		return NewIntegrationNode(base, deps.Weather)
	case "condition":
		return NewConditionNode(base)
	case "email":
		return NewEmailNode(base, deps.Email)
	default:
		return nil, fmt.Errorf("unknown node type: %s", base.NodeType)
	}
}
