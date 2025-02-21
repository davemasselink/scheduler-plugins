# Carbon Aware Scheduler

The Carbon Aware Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity and electricity pricing data.

## Features

- **Carbon-Aware Scheduling**: Schedule pods based on real-time carbon intensity data
- **Price-Aware Scheduling**: Optionally schedule pods based on electricity pricing data
- **Peak Hour Management**: Configure different thresholds for peak and off-peak hours
- **Flexible Configuration**: Extensive configuration options for fine-tuning scheduler behavior
- **Pod-Level Controls**: Pods can opt-out or specify custom thresholds via annotations
- **Caching**: Built-in caching of API responses to reduce external API calls
- **Observability**: Prometheus metrics for monitoring carbon intensity, pricing, and scheduling decisions

## Configuration

### Core Configuration

The scheduler can be configured via a YAML configuration file with the following structure:

```yaml
api:
  provider: "electricitymap"  # API provider for carbon intensity data
  key: "your-api-key"        # API authentication key
  region: "us-west-1"        # Default region for carbon intensity data
  cacheTTL: "5m"            # How long to cache API responses
  maxCacheAge: "1h"         # Maximum age of cached data

scheduling:
  baseCarbonIntensityThreshold: 200.0  # Base carbon intensity threshold (gCO2/kWh)
  maxSchedulingDelay: "24h"            # Maximum time to delay pod scheduling
  maxConcurrentPods: 10               # Maximum pods to schedule concurrently
  enablePodPriorities: true           # Enable pod priority-based scheduling

peakHours:
  enabled: true
  carbonIntensityThreshold: 300.0     # Carbon intensity threshold during peak hours
  schedules:                          # Peak hour schedules
    - dayOfWeek: "1-5"               # Monday-Friday
      startTime: "16:00"             # 4 PM
      endTime: "21:00"               # 9 PM

pricing:
  enabled: true
  provider: "genability"             # Pricing data provider
  locationId: "zone1"                # Location ID for pricing data
  peakThreshold: 0.15               # Price threshold during peak hours ($/kWh)
  offPeakThreshold: 0.10            # Price threshold during off-peak hours ($/kWh)
  maxDelay: "12h"                   # Maximum delay for price-based scheduling

observability:
  metricsEnabled: true
  metricsPort: 10259
  healthCheckEnabled: true
  healthCheckPort: 10258
  logLevel: "info"
  enableTracing: false
```

### Pod Annotations

Pods can control scheduling behavior using the following annotations:

```yaml
# Opt out of carbon-aware scheduling
carbon-aware-scheduler.kubernetes.io/skip: "true"

# Opt out of price-aware scheduling
price-aware-scheduler.kubernetes.io/skip: "true"

# Set custom carbon intensity threshold
carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"

# Set custom price threshold
price-aware-scheduler.kubernetes.io/price-threshold: "0.12"
```

## Metrics

The scheduler exports the following Prometheus metrics:

- `carbon_intensity_gauge`: Current carbon intensity for the configured region
- `electricity_rate_gauge`: Current electricity rate (when pricing is enabled)
- `scheduling_attempts_total`: Total number of scheduling attempts by result
- `pod_scheduling_latency_seconds`: Pod scheduling latency histogram
- `carbon_savings_total`: Estimated carbon savings from delayed scheduling
- `cost_savings_total`: Estimated cost savings from delayed scheduling
- `price_based_delays_total`: Number of pods delayed due to pricing thresholds

## Architecture

The scheduler consists of several key components:

1. **Main Scheduler**: Implements the Kubernetes scheduler framework interfaces
2. **API Client**: Handles communication with carbon intensity data providers
3. **Cache**: Provides caching of API responses to reduce external API calls
4. **Peak Scheduler**: Manages peak hour schedules and thresholds
5. **Pricing Provider**: Optional component for electricity pricing data

### Scheduling Logic

The scheduler follows this decision flow:

1. Check if pod has exceeded maximum scheduling delay
2. Check for opt-out annotations
3. If pricing is enabled:
   - Get current electricity rate
   - Compare against threshold (peak/off-peak)
4. Get current carbon intensity
5. Compare against threshold (base/peak)
6. Check concurrent pod limits
7. Make scheduling decision

## Development

### Running Tests

```bash
# Run all tests
make test

# Run specific test
go test -v ./pkg/carbonawarescheduler/... -run TestName

# Run tests with coverage
make test-coverage
```

### Adding a New Provider

To add a new carbon intensity or pricing data provider:

1. Create a new provider package
2. Implement the required interface
3. Add provider configuration
4. Update the provider factory
5. Add tests for the new provider

## Contributing

Please see the [contributing guide](../../CONTRIBUTING.md) for guidelines on how to contribute to this project.
