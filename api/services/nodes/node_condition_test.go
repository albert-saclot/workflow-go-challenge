package nodes_test

import (
	"context"
	"encoding/json"
	"testing"

	"workflow-code-test/api/services/nodes"
)

func TestConditionNode_Execute(t *testing.T) {
	t.Parallel()
	base := nodes.BaseFields{
		ID:       "condition",
		NodeType: "condition",
		Metadata: json.RawMessage(`{"conditionVariable":"temperature","conditionExpression":"temperature > threshold","outputVariables":["conditionMet"]}`),
	}

	tests := []struct {
		name       string
		variables  map[string]any
		wantErr    string
		wantMet    bool
		wantBranch string
	}{
		{
			name:       "greater_than met",
			variables:  map[string]any{"temperature": 30.0, "operator": "greater_than", "threshold": 25.0},
			wantMet:    true,
			wantBranch: "true",
		},
		{
			name:       "greater_than not met",
			variables:  map[string]any{"temperature": 20.0, "operator": "greater_than", "threshold": 25.0},
			wantMet:    false,
			wantBranch: "false",
		},
		{
			name:       "less_than met",
			variables:  map[string]any{"temperature": 10.0, "operator": "less_than", "threshold": 25.0},
			wantMet:    true,
			wantBranch: "true",
		},
		{
			name:       "equal_to met",
			variables:  map[string]any{"temperature": 25.0, "operator": "equal_to", "threshold": 25.0},
			wantMet:    true,
			wantBranch: "true",
		},
		{
			name:       "greater_than_or_equal at boundary",
			variables:  map[string]any{"temperature": 25.0, "operator": "greater_than_or_equal", "threshold": 25.0},
			wantMet:    true,
			wantBranch: "true",
		},
		{
			name:       "less_than_or_equal at boundary",
			variables:  map[string]any{"temperature": 25.0, "operator": "less_than_or_equal", "threshold": 25.0},
			wantMet:    true,
			wantBranch: "true",
		},
		{
			name:      "unsupported operator",
			variables: map[string]any{"temperature": 30.0, "operator": "not_equal", "threshold": 25.0},
			wantErr:   "unsupported operator: not_equal",
		},
		{
			name:      "missing condition variable",
			variables: map[string]any{"operator": "greater_than", "threshold": 25.0},
			wantErr:   "missing or invalid variable: temperature",
		},
		{
			name:       "defaults to greater_than with threshold 25",
			variables:  map[string]any{"temperature": 30.0},
			wantMet:    true,
			wantBranch: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := nodes.NewConditionNode(base)
			if err != nil {
				t.Fatalf("failed to create condition node: %v", err)
			}

			nCtx := &nodes.NodeContext{Variables: tt.variables}
			result, err := node.Execute(context.Background(), nCtx)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Branch != tt.wantBranch {
				t.Errorf("expected branch %q, got %q", tt.wantBranch, result.Branch)
			}
			met, ok := result.Output["conditionMet"].(bool)
			if !ok || met != tt.wantMet {
				t.Errorf("expected conditionMet=%v, got %v", tt.wantMet, result.Output["conditionMet"])
			}
		})
	}
}

func TestConditionNode_CustomVariable(t *testing.T) {
	t.Parallel()
	base := nodes.BaseFields{
		ID:       "condition",
		NodeType: "condition",
		Metadata: json.RawMessage(`{"conditionVariable":"discharge","outputVariables":["conditionMet"]}`),
	}

	node, err := nodes.NewConditionNode(base)
	if err != nil {
		t.Fatalf("failed to create condition node: %v", err)
	}

	nCtx := &nodes.NodeContext{Variables: map[string]any{
		"discharge": 500.0,
		"operator":  "greater_than",
		"threshold": 100.0,
	}}
	result, err := node.Execute(context.Background(), nCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Branch != "true" {
		t.Errorf("expected branch 'true', got %q", result.Branch)
	}
}
