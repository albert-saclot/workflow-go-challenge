package nodes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"workflow-code-test/api/pkg/clients/sms"
	"workflow-code-test/api/services/nodes"
	// mockSmsClient is available from the nodes_test package due to nodes_common_mocks_test.go
)

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
