package nodes

import (
	"context"
	"encoding/json"
	"fmt"

	"workflow-code-test/api/pkg/clients/sms"
)

// SmsNode sends an SMS notification using the runtime context variables.
// The message body is composed from the context, then sent via the SMS client.
type SmsNode struct {
	BaseFields
	sms sms.Client

	InputVariables  []string `json:"inputVariables"`
	OutputVariables []string `json:"outputVariables"`
}

func NewSmsNode(base BaseFields, smsClient sms.Client) (*SmsNode, error) {
	n := &SmsNode{BaseFields: base, sms: smsClient}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid sms metadata: %w", err)
	}
	return n, nil
}

func (n *SmsNode) Validate() error {
	if n.sms == nil {
		return fmt.Errorf("sms node %q: sms client is nil", n.ID)
	}
	if len(n.InputVariables) == 0 {
		return fmt.Errorf("sms node %q: no input variables", n.ID)
	}
	hasPhone := false
	for _, v := range n.InputVariables {
		if v == "phone" {
			hasPhone = true
			break
		}
	}
	if !hasPhone {
		return fmt.Errorf("sms node %q: input variables must include \"phone\"", n.ID)
	}
	return nil
}

func (n *SmsNode) Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
	phone, ok := nCtx.Variables["phone"].(string)
	if !ok || phone == "" {
		return nil, fmt.Errorf("missing or invalid variable: phone")
	}

	message, _ := nCtx.Variables["message"].(string)

	result, err := n.sms.Send(ctx, sms.Message{
		To:   phone,
		Body: message,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send sms: %w", err)
	}

	return &ExecutionResult{
		Status: "completed",
		Output: map[string]any{
			"deliveryStatus": result.DeliveryStatus,
			"smsSent":        result.Sent,
		},
	}, nil
}
