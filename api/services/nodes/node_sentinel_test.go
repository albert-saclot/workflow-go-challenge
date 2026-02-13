package nodes_test

import (
	"context"
	"strings"
	"testing"

	"workflow-code-test/api/services/nodes"
)

func TestSentinelNode_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodeType string
		wantErr  string
	}{
		{name: "valid start", nodeType: "start"},
		{name: "valid end", nodeType: "end"},
		{name: "invalid type", nodeType: "middle", wantErr: "sentinel node must be type start or end"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node := nodes.NewSentinelNode(nodes.BaseFields{ID: tt.name, NodeType: tt.nodeType})

			err := node.Validate()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

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
