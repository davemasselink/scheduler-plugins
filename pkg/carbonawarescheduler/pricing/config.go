package pricing

// FromGenericConfig creates a ProviderConfig from a generic config
func FromGenericConfig(cfg interface{}) (*ProviderConfig, error) {
	if cfg == nil {
		return &ProviderConfig{}, nil
	}

	// Convert the interface{} to our Config type
	if pricingCfg, ok := cfg.(ProviderConfig); ok {
		return &pricingCfg, nil
	}

	// Try to convert from map
	if cfgMap, ok := cfg.(map[string]interface{}); ok {
		config := &ProviderConfig{}
		if enabled, ok := cfgMap["enabled"].(bool); ok {
			config.Enabled = enabled
		}
		if provider, ok := cfgMap["provider"].(string); ok {
			config.Provider = provider
		}
		if locationID, ok := cfgMap["locationId"].(string); ok {
			config.LocationID = locationID
		}
		if apiKey, ok := cfgMap["apiKey"].(string); ok {
			config.APIKey = apiKey
		}
		if maxDelay, ok := cfgMap["maxDelay"].(string); ok {
			config.MaxDelay = maxDelay
		}
		if peak, ok := cfgMap["peakThreshold"].(float64); ok {
			config.Thresholds.Peak = peak
		}
		if offPeak, ok := cfgMap["offPeakThreshold"].(float64); ok {
			config.Thresholds.OffPeak = offPeak
		}
		return config, nil
	}

	return &ProviderConfig{}, nil
}
