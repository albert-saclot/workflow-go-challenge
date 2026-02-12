package nodes

import "context"

// SentinelNode marks the boundaries of a workflow graph (start, end).
// It preserves the raw DB metadata for ToJSON() and is a no-op on Execute().
type SentinelNode struct {
	BaseFields
}

func NewSentinelNode(base BaseFields) *SentinelNode {
	return &SentinelNode{BaseFields: base}
}

func (n *SentinelNode) Execute(_ context.Context, _ *NodeContext) (*ExecutionResult, error) {
	return &ExecutionResult{Status: "completed"}, nil
}
