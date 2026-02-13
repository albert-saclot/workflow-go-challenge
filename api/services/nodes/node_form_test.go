package nodes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"workflow-code-test/api/services/nodes"
)

func TestFormNode_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		meta    string
		wantErr string
	}{
		{
			name: "valid",
			meta: `{"inputFields":["name","email"],"outputVariables":["name","email"]}`,
		},
		{
			name:    "no input fields",
			meta:    `{"inputFields":[],"outputVariables":["name"]}`,
			wantErr: "no input fields",
		},
		{
			name:    "blank input field",
			meta:    `{"inputFields":["name"," "],"outputVariables":["name"]}`,
			wantErr: "input field [1] is blank",
		},
		{
			name:    "no output variables",
			meta:    `{"inputFields":["name"],"outputVariables":[]}`,
			wantErr: "no output variables",
		},
		{
			name:    "blank output variable",
			meta:    `{"inputFields":["name"],"outputVariables":["name",""]}`,
			wantErr: "output variable [1] is blank",
		},
		{
			name:    "input field not in output variables",
			meta:    `{"inputFields":["name","email"],"outputVariables":["name"]}`,
			wantErr: `input field "email" not listed in output variables`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{ID: "f1", NodeType: "form", Metadata: json.RawMessage(tt.meta)}
			node, err := nodes.NewFormNode(base)
			if err != nil {
				t.Fatalf("failed to create form node: %v", err)
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

func TestFormNode_Execute(t *testing.T) {
	t.Parallel()
	base := nodes.BaseFields{
		ID:       "form",
		NodeType: "form",
		Metadata: json.RawMessage(`{"inputFields":["name","city"],"outputVariables":["name","city"]}`),
	}

	tests := []struct {
		name      string
		variables map[string]any
		wantErr   string
		checkOut  func(t *testing.T, result *nodes.ExecutionResult)
	}{
		{
			name:      "all fields present",
			variables: map[string]any{"name": "Alice", "city": "Sydney"},
			checkOut: func(t *testing.T, r *nodes.ExecutionResult) {
				if r.Status != "completed" {
					t.Errorf("expected completed, got %q", r.Status)
				}
				if r.Output["name"] != "Alice" {
					t.Errorf("expected name=Alice, got %v", r.Output["name"])
				}
				if r.Output["city"] != "Sydney" {
					t.Errorf("expected city=Sydney, got %v", r.Output["city"])
				}
			},
		},
		{
			name:      "missing required field",
			variables: map[string]any{"name": "Alice"},
			wantErr:   "missing required form field: city",
		},
		{
			name:      "empty variables",
			variables: map[string]any{},
			wantErr:   "missing required form field: name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := nodes.NewFormNode(base)
			if err != nil {
				t.Fatalf("failed to create form node: %v", err)
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
			if tt.checkOut != nil {
				tt.checkOut(t, result)
			}
		})
	}
}
