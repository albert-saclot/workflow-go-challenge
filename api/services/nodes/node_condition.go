package nodes

import (
	"context"
	"encoding/json"
	"fmt"
)

// ConditionNode evaluates a condition expression against runtime variables.
// It outputs conditionMet (bool) and sets Branch to "true" or "false",
// which the execution engine uses to follow the correct outgoing edge.
type ConditionNode struct {
	base BaseFields

	ConditionExpression string   `json:"conditionExpression"`
	ConditionVariable   string   `json:"conditionVariable"`
	OutputVariables     []string `json:"outputVariables"`
}

func NewConditionNode(base BaseFields) (*ConditionNode, error) {
	n := &ConditionNode{base: base}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid condition metadata: %w", err)
	}
	return n, nil
}

func (n *ConditionNode) ToJSON() NodeJSON {
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

// Execute evaluates the condition using operator and threshold from context.
// The variable to compare is read from conditionVariable in metadata,
// defaulting to "temperature" for backward compatibility.
func (n *ConditionNode) Execute(_ context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
	varName := n.ConditionVariable
	if varName == "" {
		varName = "temperature"
	}

	value, ok := toFloat64(nCtx.Variables[varName])
	if !ok {
		return nil, fmt.Errorf("missing or invalid variable: %s", varName)
	}

	operator, _ := nCtx.Variables["operator"].(string)
	if operator == "" {
		operator = "greater_than" // default
	}

	threshold, ok := toFloat64(nCtx.Variables["threshold"])
	if !ok {
		threshold = 25 // default
	}

	conditionMet, err := evaluate(value, operator, threshold)
	if err != nil {
		return nil, err
	}

	branch := "false"
	if conditionMet {
		branch = "true"
	}

	return &ExecutionResult{
		Status: "completed",
		Branch: branch,
		Output: map[string]any{
			"conditionMet": conditionMet,
			"threshold":    threshold,
			"operator":     operator,
			"actualValue":  value,
			"message": fmt.Sprintf(
				"%s %.1f is %s %.1f - condition %s",
				varName, value, operator, threshold, branchLabel(conditionMet),
			),
		},
	}, nil
}

func evaluate(value float64, operator string, threshold float64) (bool, error) {
	switch operator {
	case "greater_than":
		return value > threshold, nil
	case "less_than":
		return value < threshold, nil
	case "equal_to":
		return value == threshold, nil
	case "greater_than_or_equal":
		return value >= threshold, nil
	case "less_than_or_equal":
		return value <= threshold, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", operator)
	}
}

func branchLabel(met bool) string {
	if met {
		return "met"
	}
	return "not met"
}

// toFloat64 converts a variable to float64, handling both float64 and
// json.Number types that may appear depending on the source.
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
