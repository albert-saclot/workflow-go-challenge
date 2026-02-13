package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"workflow-code-test/api/pkg/clients/sms"
	"workflow-code-test/api/services/nodes"
)

func TestSmsNode_Validate(t *testing.T) {
	t.Parallel()

	t.Run("nil client", func(t *testing.T) {
		t.Parallel()
		meta := `{"inputVariables":["phone"],"outputVariables":["smsSent"]}`
		base := nodes.BaseFields{ID: "sm1", NodeType: "sms", Metadata: json.RawMessage(meta)}
		node, err := nodes.NewSmsNode(base, nil)
		if err != nil {
			t.Fatalf("failed to create sms node: %v", err)
		}
		if err := node.Validate(); err == nil || !strings.Contains(err.Error(), "sms client is nil") {
			t.Errorf("expected nil-client error, got %v", err)
		}
	})

	tests := []struct {
		name    string
		meta    string
		client  *mockSmsClient
		wantErr string
	}{
		{
			name:   "valid",
			meta:   `{"inputVariables":["phone","message"],"outputVariables":["smsSent"]}`,
			client: &mockSmsClient{},
		},
		{
			name:    "no input variables",
			meta:    `{"outputVariables":["smsSent"]}`,
			client:  &mockSmsClient{},
			wantErr: "no input variables",
		},
		{
			name:    "missing phone in input variables",
			meta:    `{"inputVariables":["message"],"outputVariables":["smsSent"]}`,
			client:  &mockSmsClient{},
			wantErr: `must include "phone"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := nodes.BaseFields{ID: "sm1", NodeType: "sms", Metadata: json.RawMessage(tt.meta)}
			node, err := nodes.NewSmsNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create sms node: %v", err)
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

func TestSmsNode_Execute(t *testing.T) {
	t.Parallel()
	meta := `{"inputVariables":["phone","message"],"outputVariables":["smsSent"]}`
	base := nodes.BaseFields{ID: "sms", NodeType: "sms", Metadata: json.RawMessage(meta)}

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
			node, err := nodes.NewSmsNode(base, tt.client)
			if err != nil {
				t.Fatalf("failed to create sms node: %v", err)
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
			if result.Output["smsSent"] != true {
				t.Errorf("expected smsSent=true, got %v", result.Output["smsSent"])
			}
		})
	}
}
