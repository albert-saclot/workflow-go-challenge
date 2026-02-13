package nodes

import (
	"context"
	"fmt"
)

// SentinelNode marks the boundaries of a workflow graph (start, end).
// It preserves the raw DB metadata for ToJSON() and is a no-op on Execute().
type SentinelNode struct {
	BaseFields
}

func NewSentinelNode(base BaseFields) *SentinelNode {
	return &SentinelNode{BaseFields: base}
}

func (n *SentinelNode) Validate() error {
	if n.NodeType != "start" && n.NodeType != "end" {
		return fmt.Errorf("sentinel node must be type start or end, got %q", n.NodeType)
	}
	return nil
}

func (n *SentinelNode) Execute(_ context.Context, _ *NodeContext) (*ExecutionResult, error) {
	return &ExecutionResult{Status: "completed"}, nil
}
