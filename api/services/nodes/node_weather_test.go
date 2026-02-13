package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"workflow-code-test/api/services/nodes"
)

func TestWeatherNode_Validate(t *testing.T) {
	t.Parallel()

	validMeta := `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Sydney","lat":-33.87,"lon":151.21}]}`

	// Nil client must be tested separately: a nil *mockWeatherClient wraps
	// into a non-nil interface. Passing an untyped nil exercises the check.
	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		base := nodes.BaseFields{ID: "w1", NodeType: "integration", Metadata: json.RawMessage(validMeta)}
		node, err := nodes.NewWeatherNode(base, nil)
		if err != nil {
			t.Fatalf("failed to create weather node: %v", err)
		}
		if err := node.Validate(); err == nil || !strings.Contains(err.Error(), "weather client is nil") {
			t.Errorf("expected nil-client error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		meta    string
		client  *mockWeatherClient
		wantErr string
	}{
		{
			name:   "valid",
			meta:   validMeta,
			client: &mockWeatherClient{},
		},
		{
			name:    "missing apiEndpoint",
			meta:    `{"inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Sydney","lat":-33.87,"lon":151.21}]}`,
			client:  &mockWeatherClient{},
			wantErr: "missing apiEndpoint",
		},
		{
			name:    "no options",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[]}`,
			client:  &mockWeatherClient{},
			wantErr: "no city options configured",
		},
		{
			name:    "blank city in option",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"","lat":-33.87,"lon":151.21}]}`,
			client:  &mockWeatherClient{},
			wantErr: "blank city",
		},
		{
			name:    "latitude out of range",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Bad","lat":91.0,"lon":0}]}`,
			client:  &mockWeatherClient{},
			wantErr: "lat 91.00 out of range",
		},
		{
			name:    "longitude out of range",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Bad","lat":0,"lon":181.0}]}`,
			client:  &mockWeatherClient{},
			wantErr: "lon 181.00 out of range",
		},
		{
			name:    "no input variables",
			meta:    `{"apiEndpoint":"https://api.example.com","outputVariables":["temperature"],"options":[{"city":"Sydney","lat":-33.87,"lon":151.21}]}`,
			client:  &mockWeatherClient{},
			wantErr: "no input variables",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{ID: "w1", NodeType: "integration", Metadata: json.RawMessage(tt.meta)}
			node, err := nodes.NewWeatherNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create weather node: %v", err)
			}

			err = node.Validate()
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
