package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"workflow-code-test/api/services/nodes"
	// mockWeatherClient is available from the nodes_test package due to nodes_common_mocks_test.go
)

func TestWeatherNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"apiEndpoint":"https://example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Sydney","lat":-33.87,"lon":151.21}]}`
	base := nodes.BaseFields{ID: "weather", NodeType: "integration", Metadata: json.RawMessage(meta)}

	tests := []struct {
		name      string
		variables map[string]any
		client    *mockWeatherClient
		wantErr   string
		wantTemp  float64
	}{
		{
			name:      "success",
			variables: map[string]any{"city": "Sydney"},
			client:    &mockWeatherClient{temp: 28.5},
			wantTemp:  28.5,
		},
		{
			name:      "missing city variable",
			variables: map[string]any{},
			client:    &mockWeatherClient{},
			wantErr:   "missing required input variable: city",
		},
		{
			name:      "unsupported city",
			variables: map[string]any{"city": "Tokyo"},
			client:    &mockWeatherClient{},
			wantErr:   "unsupported city: Tokyo",
		},
		{
			name:      "api error",
			variables: map[string]any{"city": "Sydney"},
			client:    &mockWeatherClient{err: fmt.Errorf("connection refused")},
			wantErr:   "weather lookup failed: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := nodes.NewWeatherNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create weather node: %v", err)
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
			temp, ok := result.Output["temperature"].(float64)
			if !ok || temp != tt.wantTemp {
				t.Errorf("expected temperature %v, got %v", tt.wantTemp, result.Output["temperature"])
			}
		})
	}
}
