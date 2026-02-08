package nodes_test

import (
	"context"
	"testing"

	"workflow-code-test/api/services/nodes"
)

func TestSentinelNode_Execute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		nodeType string
	}{
		{name: "start", nodeType: "start"},
		{name: "end", nodeType: "end"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node := nodes.NewSentinelNode(nodes.BaseFields{ID: tt.name, NodeType: tt.nodeType})

			result, err := node.Execute(context.Background(), &nodes.NodeContext{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != "completed" {
				t.Errorf("expected completed, got %q", result.Status)
			}
		})
	}
}
