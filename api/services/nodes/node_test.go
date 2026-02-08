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
