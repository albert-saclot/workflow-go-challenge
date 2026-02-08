package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/services/nodes"
	// mockEmailClient is available from the nodes_test package due to nodes_common_mocks_test.go
)

func TestEmailNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"inputVariables":["email","city"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hello from {{city}}"}}`
	base := nodes.BaseFields{ID: "email", NodeType: "email", Metadata: json.RawMessage(meta)}

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
			node, err := nodes.NewEmailNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create email node: %v", err)
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
	base := nodes.BaseFields{ID: "email", NodeType: "email", Metadata: json.RawMessage(meta)}

	node, err := nodes.NewEmailNode(base, &mockEmailClient{result: &email.Result{Sent: true}})
	if err != nil {
		t.Fatalf("failed to create email node: %v", err)
	}

	nCtx := &nodes.NodeContext{Variables: map[string]any{"email": "a@b.com", "city": "Sydney", "name": "Alice"}}
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
