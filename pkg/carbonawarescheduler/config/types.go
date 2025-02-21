package config

import (
	"fmt"
	"time"
)

// Config holds all configuration for the carbon-aware scheduler
type Config struct {
	API           APIConfig           `yaml:"api"`
	Scheduling    SchedulingConfig    `yaml:"scheduling"`
	PeakHours     PeakHoursConfig     `yaml:"peakHours"`
	Pricing       PricingConfig       `yaml:"pricing"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// APIConfig holds configuration for external API interactions
type APIConfig struct {
	Provider    string        `yaml:"provider"`
	Key         string        `yaml:"key"`
	URL         string        `yaml:"url"`
	Region      string        `yaml:"region"`
	Timeout     time.Duration `yaml:"timeout"`
	MaxRetries  int           `yaml:"maxRetries"`
	RetryDelay  time.Duration `yaml:"retryDelay"`
	RateLimit   int           `yaml:"rateLimit"`
	CacheTTL    time.Duration `yaml:"cacheTTL"`
	MaxCacheAge time.Duration `yaml:"maxCacheAge"`
}

// SchedulingConfig holds configuration for scheduling behavior
type SchedulingConfig struct {
	BaseCarbonIntensityThreshold float64       `yaml:"baseCarbonIntensityThreshold"`
	MaxSchedulingDelay           time.Duration `yaml:"maxSchedulingDelay"`
	MaxConcurrentPods            int           `yaml:"maxConcurrentPods"`
	DefaultRegion                string        `yaml:"defaultRegion"`
	EnablePodPriorities          bool          `yaml:"enablePodPriorities"`
}

// PeakHoursConfig holds configuration for peak hour scheduling
type PeakHoursConfig struct {
	Enabled                  bool       `yaml:"enabled"`
	CarbonIntensityThreshold float64    `yaml:"carbonIntensityThreshold"`
	Schedules                []Schedule `yaml:"schedules"`
}

// Schedule defines a time range for peak hours
type Schedule struct {
	DayOfWeek string `yaml:"dayOfWeek"`
	StartTime string `yaml:"startTime"`
	EndTime   string `yaml:"endTime"`
}

// PricingConfig holds configuration for price-aware scheduling
type PricingConfig struct {
	Enabled          bool    `yaml:"enabled"`
	Provider         string  `yaml:"provider"`
	LocationID       string  `yaml:"locationId"`
	APIKey           string  `yaml:"apiKey"`
	MaxDelay         string  `yaml:"maxDelay"`
	PeakThreshold    float64 `yaml:"peakThreshold"`
	OffPeakThreshold float64 `yaml:"offPeakThreshold"`
}

// ObservabilityConfig holds configuration for monitoring and debugging
type ObservabilityConfig struct {
	MetricsEnabled     bool   `yaml:"metricsEnabled"`
	MetricsPort        int    `yaml:"metricsPort"`
	HealthCheckEnabled bool   `yaml:"healthCheckEnabled"`
	HealthCheckPort    int    `yaml:"healthCheckPort"`
	LogLevel           string `yaml:"logLevel"`
	EnableTracing      bool   `yaml:"enableTracing"`
}

// Validate performs validation of the configuration
func (c *Config) Validate() error {
	if c.API.Key == "" {
		return fmt.Errorf("API key is required")
	}

	if c.API.Provider == "" {
		return fmt.Errorf("API provider is required")
	}

	if c.Scheduling.BaseCarbonIntensityThreshold <= 0 {
		return fmt.Errorf("base carbon intensity threshold must be positive")
	}

	if c.Scheduling.MaxConcurrentPods <= 0 {
		return fmt.Errorf("max concurrent pods must be positive")
	}

	if c.PeakHours.Enabled {
		if err := c.validatePeakHours(); err != nil {
			return fmt.Errorf("invalid peak hours config: %v", err)
		}
	}

	if c.Pricing.Enabled {
		if err := c.validatePricing(); err != nil {
			return fmt.Errorf("invalid pricing config: %v", err)
		}
	}

	return nil
}

func (c *Config) validatePeakHours() error {
	if c.PeakHours.CarbonIntensityThreshold <= 0 {
		return fmt.Errorf("peak hours carbon intensity threshold must be positive")
	}

	for i, schedule := range c.PeakHours.Schedules {
		if err := validateSchedule(schedule); err != nil {
			return fmt.Errorf("invalid schedule at index %d: %v", i, err)
		}
	}

	return nil
}

func (c *Config) validatePricing() error {
	if c.Pricing.Provider == "" {
		return fmt.Errorf("pricing provider is required when pricing is enabled")
	}

	if c.Pricing.APIKey == "" {
		return fmt.Errorf("pricing API key is required when pricing is enabled")
	}

	if c.Pricing.PeakThreshold <= 0 || c.Pricing.OffPeakThreshold <= 0 {
		return fmt.Errorf("pricing thresholds must be positive")
	}

	return nil
}

func validateSchedule(schedule Schedule) error {
	// Validate day of week format
	for _, day := range schedule.DayOfWeek {
		if day < '0' || day > '6' {
			return fmt.Errorf("invalid day of week: %c (must be 0-6)", day)
		}
	}

	// Validate time format
	for _, t := range []string{schedule.StartTime, schedule.EndTime} {
		if _, err := time.Parse("15:04", t); err != nil {
			return fmt.Errorf("invalid time format: %s (must be HH:MM in 24h format)", t)
		}
	}

	return nil
}
