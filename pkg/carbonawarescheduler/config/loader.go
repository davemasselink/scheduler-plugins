package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*Config, error) {
	cfg := &Config{
		API: APIConfig{
			Key:         os.Getenv("ELECTRICITY_MAP_API_KEY"),
			URL:         getEnvOrDefault("ELECTRICITY_MAP_API_URL", "https://api.electricitymap.org/v3/carbon-intensity/latest?zone="),
			Region:      getEnvOrDefault("ELECTRICITY_MAP_API_REGION", "US-CAL-CISO"),
			Timeout:     getDurationOrDefault("API_TIMEOUT", 10*time.Second),
			MaxRetries:  getIntOrDefault("API_MAX_RETRIES", 3),
			RetryDelay:  getDurationOrDefault("API_RETRY_DELAY", 1*time.Second),
			RateLimit:   getIntOrDefault("API_RATE_LIMIT", 10),
			CacheTTL:    getDurationOrDefault("CACHE_TTL", 5*time.Minute),
			MaxCacheAge: getDurationOrDefault("MAX_CACHE_AGE", 1*time.Hour),
		},
		Scheduling: SchedulingConfig{
			BaseCarbonIntensityThreshold: getFloatOrDefault("CARBON_INTENSITY_THRESHOLD", 150.0),
			MaxSchedulingDelay:           getDurationOrDefault("MAX_SCHEDULING_DELAY", 24*time.Hour),
			DefaultRegion:                getEnvOrDefault("DEFAULT_REGION", "US-CAL-CISO"),
			EnablePodPriorities:          getBoolOrDefault("ENABLE_POD_PRIORITIES", false),
		},
		PeakHours: PeakHoursConfig{
			Enabled:                  getBoolOrDefault("PEAK_HOURS_ENABLED", false),
			CarbonIntensityThreshold: getFloatOrDefault("PEAK_CARBON_INTENSITY_THRESHOLD", 100.0),
		},
		Pricing: PricingConfig{
			Enabled:          getBoolOrDefault("PRICING_ENABLED", false),
			Provider:         os.Getenv("PRICING_PROVIDER"),
			LocationID:       os.Getenv("PRICING_LOCATION_ID"),
			APIKey:           os.Getenv("PRICING_API_KEY"),
			MaxDelay:         getEnvOrDefault("PRICING_MAX_DELAY", "6h"),
			PeakThreshold:    getFloatOrDefault("PRICING_PEAK_THRESHOLD", 0.15),
			OffPeakThreshold: getFloatOrDefault("PRICING_OFFPEAK_THRESHOLD", 0.10),
		},
		Observability: ObservabilityConfig{
			MetricsEnabled:     getBoolOrDefault("METRICS_ENABLED", true),
			MetricsPort:        getIntOrDefault("METRICS_PORT", 9090),
			HealthCheckEnabled: getBoolOrDefault("HEALTH_CHECK_ENABLED", true),
			HealthCheckPort:    getIntOrDefault("HEALTH_CHECK_PORT", 8080),
			LogLevel:           getEnvOrDefault("LOG_LEVEL", "info"),
			EnableTracing:      getBoolOrDefault("ENABLE_TRACING", false),
		},
	}

	// Load peak schedules if enabled and path provided
	if cfg.PeakHours.Enabled {
		if schedulePath := os.Getenv("PEAK_SCHEDULES_PATH"); schedulePath != "" {
			if err := loadPeakSchedules(cfg, schedulePath); err != nil {
				return nil, fmt.Errorf("failed to load peak schedules: %v", err)
			}
		}
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %v", err)
	}

	return cfg, nil
}

// Load creates a new Config from the provided runtime.Object
func Load(obj runtime.Object) (*Config, error) {
	// For now, we only support environment variable configuration
	// In the future, this could be extended to support configuration
	// from the runtime.Object parameter
	cfg, err := LoadFromEnv()
	if err != nil {
		return nil, err
	}

	klog.V(2).InfoS("Loaded configuration",
		"region", cfg.API.Region,
		"baseThreshold", cfg.Scheduling.BaseCarbonIntensityThreshold,
		"peakEnabled", cfg.PeakHours.Enabled,
		"pricingEnabled", cfg.Pricing.Enabled)

	return cfg, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getIntOrDefault(key string, defaultValue int) int {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := strconv.Atoi(strValue); err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid integer value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func getFloatOrDefault(key string, defaultValue float64) float64 {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := strconv.ParseFloat(strValue, 64); err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid float value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func getBoolOrDefault(key string, defaultValue bool) bool {
	if strValue := os.Getenv(key); strValue != "" {
		value, err := strconv.ParseBool(strValue)
		if err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid boolean value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func getDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if strValue := os.Getenv(key); strValue != "" {
		if value, err := time.ParseDuration(strValue); err == nil {
			return value
		}
		klog.V(2).InfoS("Invalid duration value, using default",
			"key", key,
			"value", strValue,
			"default", defaultValue)
	}
	return defaultValue
}

func loadPeakSchedules(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read peak schedules file: %v", err)
	}

	schedules := &PeakHoursConfig{}
	if err := yaml.Unmarshal(data, schedules); err != nil {
		return fmt.Errorf("failed to parse peak schedules: %v", err)
	}

	cfg.PeakHours.Schedules = schedules.Schedules
	return nil
}
