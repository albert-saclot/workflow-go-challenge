package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"workflow-code-test/api/pkg/clients/weather"
)

// IntegrationNode calls an external API based on its metadata configuration.
// Raw metadata is preserved for ToJSON(); parsed fields are used by Execute().
type IntegrationNode struct {
	base    BaseFields
	weather weather.Client

	// Parsed from metadata for execution
	APIEndpoint     string       `json:"apiEndpoint"`
	InputVariables  []string     `json:"inputVariables"`
	OutputVariables []string     `json:"outputVariables"`
	Options         []CityOption `json:"options"`
}

type CityOption struct {
	City string  `json:"city"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// NewIntegrationNode constructs itself from the database fields.
// Metadata is parsed into typed fields for Execute(), while the raw
// bytes are kept on base for lossless ToJSON() passthrough.
func NewIntegrationNode(base BaseFields, weatherClient weather.Client) (*IntegrationNode, error) {
	n := &IntegrationNode{base: base, weather: weatherClient}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid integration metadata: %w", err)
	}
	return n, nil
}

// ToJSON returns the React Flow representation.
// Metadata is the raw DB value — no reconstruction, no data loss.
func (n *IntegrationNode) ToJSON() NodeJSON {
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

// Execute resolves the city from context, looks up coordinates,
// and calls the weather client to fetch the current temperature.
func (n *IntegrationNode) Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
	city, ok := nCtx.Variables["city"].(string)
	if !ok {
		return nil, fmt.Errorf("missing required input variable: city")
	}

	var opt *CityOption
	for i := range n.Options {
		if strings.EqualFold(n.Options[i].City, city) {
			opt = &n.Options[i]
			break
		}
	}
	if opt == nil {
		return nil, fmt.Errorf("unsupported city: %s", city)
	}

	slog.Info("fetching weather", "city", city, "lat", opt.Lat, "lon", opt.Lon)

	temp, err := n.weather.GetTemperature(ctx, opt.Lat, opt.Lon)
	if err != nil {
		return nil, fmt.Errorf("weather lookup failed: %w", err)
	}

	slog.Info("weather result", "city", city, "temperature", temp)

	return &ExecutionResult{
		Status: "completed",
		Output: map[string]any{
			"temperature": temp,
			"location":    city,
		},
	}, nil
}
