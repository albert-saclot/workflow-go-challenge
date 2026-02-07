package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Client defines the interface for fetching weather data.
// Implementations can be swapped for testing or to use a different provider.
type Client interface {
	GetTemperature(ctx context.Context, lat, lon float64) (float64, error)
}

// OpenMeteoClient fetches weather data from the Open-Meteo API.
type OpenMeteoClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewOpenMeteoClient creates a client that talks to Open-Meteo.
// Accepts an optional http.Client for custom timeouts or transport settings.
func NewOpenMeteoClient(httpClient *http.Client) *OpenMeteoClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenMeteoClient{
		baseURL:    "https://api.open-meteo.com/v1/forecast",
		httpClient: httpClient,
	}
}

func (c *OpenMeteoClient) GetTemperature(ctx context.Context, lat, lon float64) (float64, error) {
	url := fmt.Sprintf("%s?latitude=%f&longitude=%f&current_weather=true", c.baseURL, lat, lon)

	slog.Info("calling weather API", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("weather API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("weather API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		CurrentWeather struct {
			Temperature float64 `json:"temperature"`
		} `json:"current_weather"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("failed to parse weather response: %w", err)
	}

	return result.CurrentWeather.Temperature, nil
}
