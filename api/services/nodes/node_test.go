package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/pkg/clients/flood"
	"workflow-code-test/api/pkg/clients/sms"
	"workflow-code-test/api/pkg/clients/weather"
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

func TestFormNode_Execute(t *testing.T) {
	t.Parallel()
	base := BaseFields{
		ID:       "form",
		NodeType: "form",
		Metadata: json.RawMessage(`{"inputFields":["name","city"],"outputVariables":["name","city"]}`),
	}

	tests := []struct {
		name      string
		variables map[string]any
		wantErr   string
		checkOut  func(t *testing.T, result *ExecutionResult)
	}{
		{
			name:      "all fields present",
			variables: map[string]any{"name": "Alice", "city": "Sydney"},
			checkOut: func(t *testing.T, r *ExecutionResult) {
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
			node, err := NewFormNode(base)
			if err != nil {
				t.Fatalf("failed to create form node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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

func TestConditionNode_Execute(t *testing.T) {
	t.Parallel()
	base := BaseFields{
		ID:       "condition",
		NodeType: "condition",
		Metadata: json.RawMessage(`{"conditionVariable":"temperature","conditionExpression":"temperature > threshold","outputVariables":["conditionMet"]}`),
	}

	tests := []struct {
		name      string
		variables map[string]any
		wantErr   string
		wantMet   bool
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
			node, err := NewConditionNode(base)
			if err != nil {
				t.Fatalf("failed to create condition node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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
	base := BaseFields{
		ID:       "condition",
		NodeType: "condition",
		Metadata: json.RawMessage(`{"conditionVariable":"discharge","outputVariables":["conditionMet"]}`),
	}

	node, err := NewConditionNode(base)
	if err != nil {
		t.Fatalf("failed to create condition node: %v", err)
	}

	nCtx := &NodeContext{Variables: map[string]any{
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

func TestSentinelNode_Execute(t *testing.T) {
	t.Parallel()
	node := NewSentinelNode(BaseFields{ID: "start", NodeType: "start"})

	result, err := node.Execute(context.Background(), &NodeContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %q", result.Status)
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
			base := BaseFields{
				ID:       tt.name,
				NodeType: tt.nodeType,
				Metadata: json.RawMessage(tt.metadata),
			}
			_, err := New(base, Deps{})

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestWeatherNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"apiEndpoint":"https://example.com","inputVariables":["city"],"outputVariables":["temperature"],"options":[{"city":"Sydney","lat":-33.87,"lon":151.21}]}`
	base := BaseFields{ID: "weather", NodeType: "integration", Metadata: json.RawMessage(meta)}

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
			node, err := NewWeatherNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create weather node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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

func TestEmailNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"inputVariables":["email","city"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hello from {{city}}"}}`
	base := BaseFields{ID: "email", NodeType: "email", Metadata: json.RawMessage(meta)}

	tests := []struct {
		name      string
		variables map[string]any
		client    *mockEmailClient
		wantErr   string
	}{
		{
			name:      "success",
			variables: map[string]any{"email": "alice@example.com", "city": "Sydney"},
			client:    &mockEmailClient{result: &email.Result{DeliveryStatus: "sent", Sent: true}},
		},
		{
			name:      "missing email variable",
			variables: map[string]any{"city": "Sydney"},
			client:    &mockEmailClient{},
			wantErr:   "missing or invalid variable: email",
		},
		{
			name:      "empty email variable",
			variables: map[string]any{"email": "", "city": "Sydney"},
			client:    &mockEmailClient{},
			wantErr:   "missing or invalid variable: email",
		},
		{
			name:      "send failure",
			variables: map[string]any{"email": "alice@example.com", "city": "Sydney"},
			client:    &mockEmailClient{err: fmt.Errorf("smtp error")},
			wantErr:   "failed to send email: smtp error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := NewEmailNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create email node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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
			if result.Status != "completed" {
				t.Errorf("expected status 'completed', got %q", result.Status)
			}
			if result.Output["emailSent"] != true {
				t.Errorf("expected emailSent=true, got %v", result.Output["emailSent"])
			}
		})
	}
}

func TestEmailNode_TemplateResolution(t *testing.T) {
	t.Parallel()
	meta := `{"inputVariables":["email","city","name"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hi {{name}}, the weather in {{city}} is nice."}}`
	base := BaseFields{ID: "email", NodeType: "email", Metadata: json.RawMessage(meta)}

	node, err := NewEmailNode(base, &mockEmailClient{result: &email.Result{Sent: true}})
	if err != nil {
		t.Fatalf("failed to create email node: %v", err)
	}

	nCtx := &NodeContext{Variables: map[string]any{"email": "a@b.com", "city": "Sydney", "name": "Alice"}}
	result, err := node.Execute(context.Background(), nCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft, ok := result.Output["emailDraft"].(map[string]any)
	if !ok {
		t.Fatal("expected emailDraft in output")
	}
	if draft["subject"] != "Weather in Sydney" {
		t.Errorf("expected subject 'Weather in Sydney', got %q", draft["subject"])
	}
	if draft["body"] != "Hi Alice, the weather in Sydney is nice." {
		t.Errorf("unexpected body: %q", draft["body"])
	}
}

func TestSmsNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"inputVariables":["phone","message"],"outputVariables":["smsSent"]}`
	base := BaseFields{ID: "sms", NodeType: "sms", Metadata: json.RawMessage(meta)}

	tests := []struct {
		name      string
		variables map[string]any
		client    *mockSmsClient
		wantErr   string
	}{
		{
			name:      "success",
			variables: map[string]any{"phone": "+61400000000", "message": "flood alert"},
			client:    &mockSmsClient{result: &sms.Result{DeliveryStatus: "sent", Sent: true}},
		},
		{
			name:      "missing phone variable",
			variables: map[string]any{"message": "flood alert"},
			client:    &mockSmsClient{},
			wantErr:   "missing or invalid variable: phone",
		},
		{
			name:      "send failure",
			variables: map[string]any{"phone": "+61400000000", "message": "flood alert"},
			client:    &mockSmsClient{err: fmt.Errorf("provider error")},
			wantErr:   "failed to send sms: provider error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			node, err := NewSmsNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create sms node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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
			if result.Output["smsSent"] != true {
				t.Errorf("expected smsSent=true, got %v", result.Output["smsSent"])
			}
		})
	}
}

func TestFloodNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"apiEndpoint":"https://example.com","inputVariables":["city"],"outputVariables":["floodRisk","discharge"],"options":[{"city":"Brisbane","lat":-27.47,"lon":153.03}]}`
	base := BaseFields{ID: "flood", NodeType: "flood", Metadata: json.RawMessage(meta)}

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
			node, err := NewFloodNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create flood node: %v", err)
			}

			nCtx := &NodeContext{Variables: tt.variables}
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

func TestToFloat64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   any
		want    float64
		wantOK  bool
	}{
		{name: "float64", input: 42.5, want: 42.5, wantOK: true},
		{name: "float32", input: float32(42.5), want: 42.5, wantOK: true},
		{name: "int", input: 42, want: 42.0, wantOK: true},
		{name: "json.Number", input: json.Number("42.5"), want: 42.5, wantOK: true},
		{name: "string fails", input: "42.5", want: 0, wantOK: false},
		{name: "nil fails", input: nil, want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := toFloat64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("toFloat64(%v): ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("toFloat64(%v)=%v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
