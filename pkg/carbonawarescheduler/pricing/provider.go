package pricing

import (
	"context"
	"fmt"
)

// Provider defines the interface for electricity pricing providers
type Provider interface {
	// GetCurrentRate returns the current electricity rate in $/kWh
	GetCurrentRate(ctx context.Context, locationID string) (float64, error)

	// IsPeakPeriod checks if the current time is within a peak pricing period
	// Returns: isPeak, currentRate, error
	IsPeakPeriod(ctx context.Context, locationID string) (bool, float64, error)
}

// ProviderConfig contains configuration for electricity pricing
type ProviderConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Provider   string `yaml:"provider"`   // e.g., "genability"
	LocationID string `yaml:"locationId"` // Utility/tariff identifier
	APIKey     string `yaml:"apiKey"`
	MaxDelay   string `yaml:"maxDelay"`
	Thresholds struct {
		Peak    float64 `yaml:"peak"`
		OffPeak float64 `yaml:"offPeak"`
	} `yaml:"thresholds"`
}

// NewProvider creates a new pricing provider based on configuration
func NewProvider(config *ProviderConfig) (Provider, error) {
	switch config.Provider {
	case "genability":
		if config.APIKey == "" {
			return nil, fmt.Errorf("Genability API key is required")
		}
		return NewGenabilityClient(config.APIKey), nil
	default:
		return nil, fmt.Errorf("unsupported pricing provider: %s", config.Provider)
	}
}
