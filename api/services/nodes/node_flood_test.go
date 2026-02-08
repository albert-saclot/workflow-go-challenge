package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"workflow-code-test/api/pkg/clients/flood"
	"workflow-code-test/api/services/nodes"
	// mockFloodClient is available from the nodes_test package due to nodes_common_mocks_test.go
)

func TestFloodNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"apiEndpoint":"https://example.com","inputVariables":["city"],"outputVariables":["floodRisk","discharge"],"options":[{"city":"Brisbane","lat":-27.47,"lon":153.03}]}`
	base := nodes.BaseFields{ID: "flood", NodeType: "flood", Metadata: json.RawMessage(meta)}

	tests := []struct {
		name      string
		variables map[string]any
		client    *mockFloodClient
		wantErr   string
		wantRisk  string
	}{
		{
			name:      "success",
			variables: map[string]any{"city": "Brisbane"},
			client:    &mockFloodClient{result: &flood.Result{Discharge: 250.0, RiskLevel: "moderate"}},
			wantRisk:  "moderate",
		},
		{
			name:      "missing city variable",
			variables: map[string]any{},
			client:    &mockFloodClient{},
			wantErr:   "missing required input variable: city",
		},
		{
			name:      "unsupported city",
			variables: map[string]any{"city": "London"},
			client:    &mockFloodClient{},
			wantErr:   "unsupported city: London",
		},
		{
			name:      "api error",
			variables: map[string]any{"city": "Brisbane"},
			client:    &mockFloodClient{err: fmt.Errorf("timeout")},
			wantErr:   "flood risk lookup failed: timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := nodes.NewFloodNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create flood node: %v", err)
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
			if result.Output["floodRisk"] != tt.wantRisk {
				t.Errorf("expected risk %q, got %v", tt.wantRisk, result.Output["floodRisk"])
			}
		})
	}
}
