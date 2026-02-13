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

func (n *FloodNode) Validate() error {
	if n.flood == nil {
		return fmt.Errorf("flood node %q: flood client is nil", n.ID)
	}
	if n.APIEndpoint == "" {
		return fmt.Errorf("flood node %q: missing apiEndpoint", n.ID)
	}
	if len(n.Options) == 0 {
		return fmt.Errorf("flood node %q: no city options configured", n.ID)
	}
	for i, opt := range n.Options {
		if strings.TrimSpace(opt.City) == "" {
			return fmt.Errorf("flood node %q: option [%d] has blank city", n.ID, i)
		}
		if opt.Lat < -90 || opt.Lat > 90 {
			return fmt.Errorf("flood node %q: option %q lat %.2f out of range [-90, 90]", n.ID, opt.City, opt.Lat)
		}
		if opt.Lon < -180 || opt.Lon > 180 {
			return fmt.Errorf("flood node %q: option %q lon %.2f out of range [-180, 180]", n.ID, opt.City, opt.Lon)
		}
	}
	if len(n.InputVariables) == 0 {
		return fmt.Errorf("flood node %q: no input variables", n.ID)
	}
	return nil
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
