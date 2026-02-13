package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"workflow-code-test/api/pkg/clients/flood"
	"workflow-code-test/api/services/nodes"
)

func TestFloodNode_Validate(t *testing.T) {
	t.Parallel()

	validMeta := `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["floodRisk"],"options":[{"city":"Brisbane","lat":-27.47,"lon":153.03}]}`

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		base := nodes.BaseFields{ID: "fl1", NodeType: "flood", Metadata: json.RawMessage(validMeta)}
		node, err := nodes.NewFloodNode(base, nil)
		if err != nil {
			t.Fatalf("failed to create flood node: %v", err)
		}
		if err := node.Validate(); err == nil || !strings.Contains(err.Error(), "flood client is nil") {
			t.Errorf("expected nil-client error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		meta    string
		client  *mockFloodClient
		wantErr string
	}{
		{
			name:   "valid",
			meta:   validMeta,
			client: &mockFloodClient{},
		},
		{
			name:    "missing apiEndpoint",
			meta:    `{"inputVariables":["city"],"outputVariables":["floodRisk"],"options":[{"city":"Brisbane","lat":-27.47,"lon":153.03}]}`,
			client:  &mockFloodClient{},
			wantErr: "missing apiEndpoint",
		},
		{
			name:    "no options",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["floodRisk"],"options":[]}`,
			client:  &mockFloodClient{},
			wantErr: "no city options configured",
		},
		{
			name:    "blank city",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["floodRisk"],"options":[{"city":" ","lat":-27.47,"lon":153.03}]}`,
			client:  &mockFloodClient{},
			wantErr: "blank city",
		},
		{
			name:    "latitude out of range",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["floodRisk"],"options":[{"city":"Bad","lat":-91.0,"lon":0}]}`,
			client:  &mockFloodClient{},
			wantErr: "lat -91.00 out of range",
		},
		{
			name:    "longitude out of range",
			meta:    `{"apiEndpoint":"https://api.example.com","inputVariables":["city"],"outputVariables":["floodRisk"],"options":[{"city":"Bad","lat":0,"lon":-181.0}]}`,
			client:  &mockFloodClient{},
			wantErr: "lon -181.00 out of range",
		},
		{
			name:    "no input variables",
			meta:    `{"apiEndpoint":"https://api.example.com","outputVariables":["floodRisk"],"options":[{"city":"Brisbane","lat":-27.47,"lon":153.03}]}`,
			client:  &mockFloodClient{},
			wantErr: "no input variables",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{ID: "fl1", NodeType: "flood", Metadata: json.RawMessage(tt.meta)}
			node, err := nodes.NewFloodNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create flood node: %v", err)
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
