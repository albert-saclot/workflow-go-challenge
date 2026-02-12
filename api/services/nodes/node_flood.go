package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"workflow-code-test/api/pkg/clients/flood"
)

// FloodNode checks flood risk for a location via the flood client.
// Follows the same pattern as WeatherNode — resolves city from context,
// looks up coordinates, and delegates the API call to the client.
type FloodNode struct {
	BaseFields
	flood flood.Client

	APIEndpoint     string       `json:"apiEndpoint"`
	InputVariables  []string     `json:"inputVariables"`
	OutputVariables []string     `json:"outputVariables"`
	Options         []CityOption `json:"options"`
}

func NewFloodNode(base BaseFields, floodClient flood.Client) (*FloodNode, error) {
	n := &FloodNode{BaseFields: base, flood: floodClient}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid flood metadata: %w", err)
	}
	return n, nil
}

func (n *FloodNode) Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
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

	slog.Debug("fetching flood risk", "city", city, "lat", opt.Lat, "lon", opt.Lon)

	result, err := n.flood.GetFloodRisk(ctx, opt.Lat, opt.Lon)
	if err != nil {
		return nil, fmt.Errorf("flood risk lookup failed: %w", err)
	}

	slog.Debug("flood risk result", "city", city, "risk", result.RiskLevel, "discharge", result.Discharge)

	return &ExecutionResult{
		Status: "completed",
		Output: map[string]any{
			"floodRisk": result.RiskLevel,
			"discharge": result.Discharge,
			"location":  city,
		},
	}, nil
}
