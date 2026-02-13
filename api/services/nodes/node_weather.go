package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"workflow-code-test/api/pkg/clients/weather"
)

// WeatherNode calls an external API based on its metadata configuration.
// Raw metadata is preserved for ToJSON(); parsed fields are used by Execute().
type WeatherNode struct {
	BaseFields
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

// NewWeatherNode constructs itself from the database fields.
// Metadata is parsed into typed fields for Execute(), while the raw
// bytes are kept on base for lossless ToJSON() passthrough.
func NewWeatherNode(base BaseFields, weatherClient weather.Client) (*WeatherNode, error) {
	n := &WeatherNode{BaseFields: base, weather: weatherClient}
	if err := json.Unmarshal(base.Metadata, n); err != nil {
		return nil, fmt.Errorf("invalid integration metadata: %w", err)
	}
	return n, nil
}

func (n *WeatherNode) Validate() error {
	if n.weather == nil {
		return fmt.Errorf("weather node %q: weather client is nil", n.ID)
	}
	if n.APIEndpoint == "" {
		return fmt.Errorf("weather node %q: missing apiEndpoint", n.ID)
	}
	if len(n.Options) == 0 {
		return fmt.Errorf("weather node %q: no city options configured", n.ID)
	}
	for i, opt := range n.Options {
		if strings.TrimSpace(opt.City) == "" {
			return fmt.Errorf("weather node %q: option [%d] has blank city", n.ID, i)
		}
		if opt.Lat < -90 || opt.Lat > 90 {
			return fmt.Errorf("weather node %q: option %q lat %.2f out of range [-90, 90]", n.ID, opt.City, opt.Lat)
		}
		if opt.Lon < -180 || opt.Lon > 180 {
			return fmt.Errorf("weather node %q: option %q lon %.2f out of range [-180, 180]", n.ID, opt.City, opt.Lon)
		}
	}
	if len(n.InputVariables) == 0 {
		return fmt.Errorf("weather node %q: no input variables", n.ID)
	}
	return nil
}

// Execute resolves the city from context, looks up coordinates,
// and calls the weather client to fetch the current temperature.
func (n *WeatherNode) Execute(ctx context.Context, nCtx *NodeContext) (*ExecutionResult, error) {
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

	slog.Debug("fetching weather", "city", city, "lat", opt.Lat, "lon", opt.Lon)

	temp, err := n.weather.GetTemperature(ctx, opt.Lat, opt.Lon)
	if err != nil {
		return nil, fmt.Errorf("weather lookup failed: %w", err)
	}

	slog.Debug("weather result", "city", city, "temperature", temp)

	return &ExecutionResult{
		Status: "completed",
		Output: map[string]any{
			"temperature": temp,
			"location":    city,
		},
	}, nil
}
