package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/services/nodes"
)

func TestEmailNode_Validate(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		meta := `{"inputVariables":["email","city"],"emailTemplate":{"subject":"hi","body":"hello"}}`
		base := nodes.BaseFields{ID: "em1", NodeType: "email", Metadata: json.RawMessage(meta)}
		node, err := nodes.NewEmailNode(base, nil)
		if err != nil {
			t.Fatalf("failed to create email node: %v", err)
		}
		if err := node.Validate(); err == nil || !strings.Contains(err.Error(), "email client is nil") {
			t.Errorf("expected nil-client error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		meta    string
		client  *mockEmailClient
		wantErr string
	}{
		{
			name:   "valid",
			meta:   `{"inputVariables":["email","city"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hello from {{city}}"}}`,
			client: &mockEmailClient{},
		},
		{
			name:    "missing subject",
			meta:    `{"inputVariables":["email"],"emailTemplate":{"subject":"","body":"hello"}}`,
			client:  &mockEmailClient{},
			wantErr: "missing email template subject",
		},
		{
			name:    "missing body",
			meta:    `{"inputVariables":["email"],"emailTemplate":{"subject":"hi","body":""}}`,
			client:  &mockEmailClient{},
			wantErr: "missing email template body",
		},
		{
			name:    "no input variables",
			meta:    `{"emailTemplate":{"subject":"hi","body":"hello"}}`,
			client:  &mockEmailClient{},
			wantErr: "no input variables",
		},
		{
			name:    "template placeholder not in input variables",
			meta:    `{"inputVariables":["email"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hello"}}`,
			client:  &mockEmailClient{},
			wantErr: "template references {{city}} not in input variables",
		},
		{
			name:   "template with all placeholders declared",
			meta:   `{"inputVariables":["email","city","name"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hi {{name}}"}}`,
			client: &mockEmailClient{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{ID: "em1", NodeType: "email", Metadata: json.RawMessage(tt.meta)}
			node, err := nodes.NewEmailNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create email node: %v", err)
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

func TestEmailNode_Execute(t *testing.T) {
	t.Parallel()
	defaultMeta := `{"inputVariables":["email","city"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hello from {{city}}"}}`

	tests := []struct {
		name      string
		metadata  string
		variables map[string]any
		client    *mockEmailClient
		wantErr   string
		checkOut  func(t *testing.T, result *nodes.ExecutionResult)
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
		{
			name:      "template resolution",
			metadata:  `{"inputVariables":["email","city","name"],"outputVariables":["emailSent"],"emailTemplate":{"subject":"Weather in {{city}}","body":"Hi {{name}}, the weather in {{city}} is nice."}}`,
			variables: map[string]any{"email": "a@b.com", "city": "Sydney", "name": "Alice"},
			client:    &mockEmailClient{result: &email.Result{Sent: true}},
			checkOut: func(t *testing.T, result *nodes.ExecutionResult) {
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
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			meta := defaultMeta
			if tt.metadata != "" {
				meta = tt.metadata
			}
			base := nodes.BaseFields{ID: "email", NodeType: "email", Metadata: json.RawMessage(meta)}

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
			if tt.checkOut != nil {
				tt.checkOut(t, result)
			}
		})
	}
}
