package nodes_test

import (
	"context"
	"encoding/json"
	"testing"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/pkg/clients/flood"
	"workflow-code-test/api/pkg/clients/sms"
	"workflow-code-test/api/pkg/clients/weather"
	"workflow-code-test/api/services/nodes"
)

// Mock clients for node execution tests

type mockWeatherClient struct {
	temp float64
	err  error
}

func (m *mockWeatherClient) GetTemperature(_ context.Context, _, _ float64) (float64, error) {
	return m.temp, m.err
}

type mockEmailClient struct {
	result *email.Result
	err    error
}

func (m *mockEmailClient) Send(_ context.Context, _ email.Message) (*email.Result, error) {
	return m.result, m.err
}

type mockSmsClient struct {
	result *sms.Result
	err    error
}

func (m *mockSmsClient) Send(_ context.Context, _ sms.Message) (*sms.Result, error) {
	return m.result, m.err
}

type mockFloodClient struct {
	result *flood.Result
	err    error
}

func (m *mockFloodClient) GetFloodRisk(_ context.Context, _, _ float64) (*flood.Result, error) {
	return m.result, m.err
}

// Ensure mocks satisfy interfaces at compile time.
var (
	_ weather.Client = (*mockWeatherClient)(nil)
	_ email.Client   = (*mockEmailClient)(nil)
	_ sms.Client     = (*mockSmsClient)(nil)
	_ flood.Client   = (*mockFloodClient)(nil)
)

func TestNodeToJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nodeType string
		base     nodes.BaseFields
		deps     nodes.Deps
	}{
		{
			name:     "start",
			nodeType: "start",
			base: nodes.BaseFields{
				ID: "s1", NodeType: "start",
				Position:    nodes.Position{X: 10, Y: 20},
				Label:       "Begin", Description: "entry point",
				Metadata: json.RawMessage(`{}`),
			},
		},
		{
			name:     "end",
			nodeType: "end",
			base: nodes.BaseFields{
				ID: "e1", NodeType: "end",
				Position:    nodes.Position{X: 900, Y: 500},
				Label:       "Finish", Description: "terminal",
				Metadata: json.RawMessage(`{}`),
			},
		},
		{
			name:     "form",
			nodeType: "form",
			base: nodes.BaseFields{
				ID: "f1", NodeType: "form",
				Position:    nodes.Position{X: 100, Y: 200},
				Label:       "User Input", Description: "collects data",
				Metadata: json.RawMessage(`{"inputFields":["name"]}`),
			},
		},
		{
			name:     "condition",
			nodeType: "condition",
			base: nodes.BaseFields{
				ID: "c1", NodeType: "condition",
				Position:    nodes.Position{X: 300, Y: 150},
				Label:       "Check Temp", Description: "branches on temp",
				Metadata: json.RawMessage(`{"conditionVariable":"temperature"}`),
			},
		},
		{
			name:     "integration",
			nodeType: "integration",
			base: nodes.BaseFields{
				ID: "i1", NodeType: "integration",
				Position:    nodes.Position{X: 200, Y: 300},
				Label:       "Weather API", Description: "fetches temperature",
				Metadata: json.RawMessage(`{"inputVariables":["lat","lon"],"outputVariables":["temperature"]}`),
			},
			deps: nodes.Deps{Weather: &mockWeatherClient{}},
		},
		{
			name:     "email",
			nodeType: "email",
			base: nodes.BaseFields{
				ID: "em1", NodeType: "email",
				Position:    nodes.Position{X: 400, Y: 100},
				Label:       "Send Email", Description: "notification",
				Metadata: json.RawMessage(`{"inputVariables":["email"],"emailTemplate":{"subject":"hi","body":"hello"}}`),
			},
			deps: nodes.Deps{Email: &mockEmailClient{}},
		},
		{
			name:     "sms",
			nodeType: "sms",
			base: nodes.BaseFields{
				ID: "sm1", NodeType: "sms",
				Position:    nodes.Position{X: 500, Y: 250},
				Label:       "Send SMS", Description: "text alert",
				Metadata: json.RawMessage(`{"inputVariables":["phone"],"smsTemplate":{"body":"alert"}}`),
			},
			deps: nodes.Deps{SMS: &mockSmsClient{}},
		},
		{
			name:     "flood",
			nodeType: "flood",
			base: nodes.BaseFields{
				ID: "fl1", NodeType: "flood",
				Position:    nodes.Position{X: 600, Y: 350},
				Label:       "Flood Check", Description: "risk assessment",
				Metadata: json.RawMessage(`{"inputVariables":["lat","lon"],"outputVariables":["floodRisk"]}`),
			},
			deps: nodes.Deps{Flood: &mockFloodClient{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := nodes.New(tt.base, tt.deps)
			if err != nil {
				t.Fatalf("failed to create %s node: %v", tt.nodeType, err)
			}

			got := node.ToJSON()

			if got.ID != tt.base.ID {
				t.Errorf("ID: want %q, got %q", tt.base.ID, got.ID)
			}
			if got.Type != tt.base.NodeType {
				t.Errorf("Type: want %q, got %q", tt.base.NodeType, got.Type)
			}
			if got.Position != tt.base.Position {
				t.Errorf("Position: want %+v, got %+v", tt.base.Position, got.Position)
			}
			if got.Data.Label != tt.base.Label {
				t.Errorf("Label: want %q, got %q", tt.base.Label, got.Data.Label)
			}
			if got.Data.Description != tt.base.Description {
				t.Errorf("Description: want %q, got %q", tt.base.Description, got.Data.Description)
			}
			if string(got.Data.Metadata) != string(tt.base.Metadata) {
				t.Errorf("Metadata: want %s, got %s", tt.base.Metadata, got.Data.Metadata)
			}
		})
	}
}

func TestNodeFactory(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		nodeType string
		metadata string
		wantErr  bool
	}{
		{name: "start", nodeType: "start", metadata: `{}`},
		{name: "end", nodeType: "end", metadata: `{}`},
		{name: "form", nodeType: "form", metadata: `{"inputFields":["name"]}`},
		{name: "condition", nodeType: "condition", metadata: `{"conditionVariable":"temp"}`},
		{name: "unknown type", nodeType: "foobar", metadata: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{
				ID:       tt.name,
				NodeType: tt.nodeType,
				Metadata: json.RawMessage(tt.metadata),
			}
			_, err := nodes.New(base, nodes.Deps{})

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
