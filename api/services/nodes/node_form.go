package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FormNode collects user input. The metadata defines which fields to
// collect (inputFields) and which variables they produce (outputVariables).
// During execution, it reads the expected fields from the runtime context
// (pre-populated from the execute request payload).
type FormNode struct {
	BaseFields

	InputFields     []string `json:"inputFields"`
	OutputVariables []string `json:"outputVariables"`
}

func NewFormNode(base BaseFields) (*FormNode, error) {
	n := &FormNode{BaseFields: base}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid form metadata: %w", err)
	}
	return n, nil
}

func (n *FormNode) Validate() error {
	if len(n.InputFields) == 0 {
		return fmt.Errorf("form node %q: no input fields", n.ID)
	}
	for i, f := range n.InputFields {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("form node %q: input field [%d] is blank", n.ID, i)
		}
	}
	if len(n.OutputVariables) == 0 {
		return fmt.Errorf("form node %q: no output variables", n.ID)
	}
	for i, v := range n.OutputVariables {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("form node %q: output variable [%d] is blank", n.ID, i)
		}
	}
	// Every input field must appear in output variables so the values flow downstream.
	outSet := make(map[string]bool, len(n.OutputVariables))
	for _, v := range n.OutputVariables {
		outSet[v] = true
	}
	for _, f := range n.InputFields {
		if !outSet[f] {
			return fmt.Errorf("form node %q: input field %q not listed in output variables", n.ID, f)
		}
	}
	return nil
}

// Execute extracts the declared input fields from the runtime context
// and passes them through as output variables for downstream nodes.
func (n *FormNode) Execute(_ context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
	output := make(map[string]any)

	for _, field := range n.InputFields {
		val, ok := nCtx.Variables[field]
		if !ok {
			return nil, fmt.Errorf("missing required form field: %s", field)
		}
		output[field] = val
	}

	return &ExecutionResult{
		Status: "completed",
		Output: output,
	}, nil
}
