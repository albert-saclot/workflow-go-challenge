package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
)

// helper to build a minimal workflow with given nodes and edges
func buildWorkflow(ns []storage.Node, es []storage.Edge) *storage.Workflow {
	return &storage.Workflow{
		ID:    [16]byte{1},
		Name:  "test",
		Nodes: ns,
		Edges: es,
	}
}

func node(id, typ string) storage.Node {
	return storage.Node{
		ID:       id,
		Type:     typ,
		Position: storage.NodePosition{X: 0, Y: 0},
		Data: storage.NodeData{
			Label:       id,
			Description: id,
			Metadata:    json.RawMessage(`{}`),
		},
	}
}

func edge(id, source, target string, sourceHandle *string) storage.Edge {
	return storage.Edge{
		ID:           id,
		Source:       source,
		Target:       target,
		Type:         "smoothstep",
		SourceHandle: sourceHandle,
	}
}

func strPtr(s string) *string { return &s }

func TestExecuteWorkflow(t *testing.T) {
	tests := []struct {
		name       string
		nodes      []storage.Node
		edges      []storage.Edge
		inputs     map[string]any
		wantStatus string
		wantSteps  int
		wantError  bool   // hard error (nil result)
		wantFailed string // failedNode value
	}{
		{
			name:       "start to end",
			nodes:      []storage.Node{node("start", "start"), node("end", "end")},
			edges:      []storage.Edge{edge("e1", "start", "end", nil)},
			inputs:     map[string]any{},
			wantStatus: "completed",
			wantSteps:  2,
		},
		{
			name:       "no start node",
			nodes:      []storage.Node{node("end", "end")},
			edges:      []storage.Edge{},
			inputs:     map[string]any{},
			wantError:  true,
		},
		{
			name: "cycle detection",
			nodes: []storage.Node{
				node("start", "start"),
				node("a", "start"), // using start type since it's a passthrough
				node("b", "start"),
			},
			edges: []storage.Edge{
				edge("e1", "start", "a", nil),
				edge("e2", "a", "b", nil),
				edge("e3", "b", "a", nil), // cycle: b → a
			},
			inputs:     map[string]any{},
			wantStatus: "failed",
			wantSteps:  3, // start, a, b — then cycle detected at a
			wantFailed: "a",
		},
		{
			name: "node failure returns partial results",
			nodes: []storage.Node{
				node("start", "start"),
				{
					ID:       "form",
					Type:     "form",
					Position: storage.NodePosition{},
					Data: storage.NodeData{
						Label:       "Form",
						Description: "Form",
						Metadata:    json.RawMessage(`{"inputFields":["name"],"outputVariables":["name"]}`),
					},
				},
				node("end", "end"),
			},
			edges: []storage.Edge{
				edge("e1", "start", "form", nil),
				edge("e2", "form", "end", nil),
			},
			inputs:     map[string]any{}, // missing "name" → form will fail
			wantStatus: "failed",
			wantSteps:  2, // start (completed) + form (error)
			wantFailed: "form",
		},
		{
			name: "condition branches to true path",
			nodes: []storage.Node{
				node("start", "start"),
				{
					ID:   "cond",
					Type: "condition",
					Data: storage.NodeData{
						Label:    "Cond",
						Metadata: json.RawMessage(`{"conditionExpression":"temperature > threshold","outputVariables":["conditionMet"]}`),
					},
				},
				node("yes", "end"),
				node("no", "end"),
			},
			edges: []storage.Edge{
				edge("e1", "start", "cond", nil),
				edge("e2", "cond", "yes", strPtr("true")),
				edge("e3", "cond", "no", strPtr("false")),
			},
			inputs:     map[string]any{"temperature": 30.0, "operator": "greater_than", "threshold": 25.0},
			wantStatus: "completed",
			wantSteps:  3, // start, cond, yes
		},
		{
			name: "condition branches to false path",
			nodes: []storage.Node{
				node("start", "start"),
				{
					ID:   "cond",
					Type: "condition",
					Data: storage.NodeData{
						Label:    "Cond",
						Metadata: json.RawMessage(`{"conditionExpression":"temperature > threshold","outputVariables":["conditionMet"]}`),
					},
				},
				node("yes", "end"),
				node("no", "end"),
			},
			edges: []storage.Edge{
				edge("e1", "start", "cond", nil),
				edge("e2", "cond", "yes", strPtr("true")),
				edge("e3", "cond", "no", strPtr("false")),
			},
			inputs:     map[string]any{"temperature": 10.0, "operator": "greater_than", "threshold": 25.0},
			wantStatus: "completed",
			wantSteps:  3, // start, cond, no
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := buildWorkflow(tt.nodes, tt.edges)
			result, err := executeWorkflow(context.Background(), wf, tt.inputs, nodes.Deps{})

			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", result.Status, tt.wantStatus)
			}
			if len(result.Steps) != tt.wantSteps {
				t.Errorf("steps: got %d, want %d", len(result.Steps), tt.wantSteps)
			}
			if tt.wantFailed != "" && result.FailedNode != tt.wantFailed {
				t.Errorf("failedNode: got %q, want %q", result.FailedNode, tt.wantFailed)
			}
		})
	}
}

func TestExecuteWorkflow_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	wf := buildWorkflow(
		[]storage.Node{node("start", "start"), node("end", "end")},
		[]storage.Edge{edge("e1", "start", "end", nil)},
	)

	result, err := executeWorkflow(ctx, wf, nil, nodes.Deps{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "cancelled" {
		t.Errorf("status: got %q, want 'cancelled'", result.Status)
	}
}

func TestNextNode(t *testing.T) {
	tests := []struct {
		name   string
		edges  []edgeTarget
		branch string
		want   string
	}{
		{
			name:  "no edges returns empty",
			edges: nil,
			want:  "",
		},
		{
			name:   "single edge no branch",
			edges:  []edgeTarget{{targetID: "next", sourceHandle: nil}},
			branch: "",
			want:   "next",
		},
		{
			name: "branch matches true",
			edges: []edgeTarget{
				{targetID: "yes", sourceHandle: strPtr("true")},
				{targetID: "no", sourceHandle: strPtr("false")},
			},
			branch: "true",
			want:   "yes",
		},
		{
			name: "branch matches false",
			edges: []edgeTarget{
				{targetID: "yes", sourceHandle: strPtr("true")},
				{targetID: "no", sourceHandle: strPtr("false")},
			},
			branch: "false",
			want:   "no",
		},
		{
			name: "unmatched branch returns empty",
			edges: []edgeTarget{
				{targetID: "yes", sourceHandle: strPtr("true")},
			},
			branch: "false",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextNode(tt.edges, tt.branch)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
