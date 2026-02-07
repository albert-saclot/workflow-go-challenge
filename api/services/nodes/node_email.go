package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workflow-code-test/api/pkg/clients/email"
)

// EmailNode composes and sends an email using a template from metadata.
// Variable placeholders like {{city}} in the template are resolved from
// the runtime context. The actual send is delegated to the email client.
type EmailNode struct {
	base  BaseFields
	email email.Client

	InputVariables  []string      `json:"inputVariables"`
	OutputVariables []string      `json:"outputVariables"`
	EmailTemplate   EmailTemplate `json:"emailTemplate"`
}

type EmailTemplate struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func NewEmailNode(base BaseFields, emailClient email.Client) (*EmailNode, error) {
	n := &EmailNode{base: base, email: emailClient}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid email metadata: %w", err)
	}
	return n, nil
}

func (n *EmailNode) ToJSON() NodeJSON {
	return NodeJSON{
		ID:       n.base.ID,
		Type:     n.base.NodeType,
		Position: n.base.Position,
		Data: NodeData{
			Label:       n.base.Label,
			Description: n.base.Description,
			Metadata:    n.base.Metadata,
		},
	}
}

// Execute resolves template placeholders from context variables and
// sends the email via the client. Returns the composed email as output.
func (n *EmailNode) Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
	subject := resolveTemplate(n.EmailTemplate.Subject, nCtx.Variables)
	body := resolveTemplate(n.EmailTemplate.Body, nCtx.Variables)
	to, _ := nCtx.Variables["email"].(string)

	msg := email.Message{
		To:      to,
		From:    "weather-alerts@example.com",
		Subject: subject,
		Body:    body,
	}

	result, err := n.email.Send(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to send email: %w", err)
	}

	return &ExecutionResult{
		Status: "completed",
		Output: map[string]any{
			"emailDraft": map[string]any{
				"to":      msg.To,
				"from":    msg.From,
				"subject": msg.Subject,
				"body":    msg.Body,
			},
			"deliveryStatus": result.DeliveryStatus,
			"emailSent":      result.Sent,
		},
	}, nil
}

// resolveTemplate replaces {{key}} placeholders with values from variables.
func resolveTemplate(tmpl string, vars map[string]any) string {
	result := tmpl
	for key, val := range vars {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
	}
	return result
}
