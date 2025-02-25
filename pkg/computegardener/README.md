# Carbon Aware Scheduler

The Carbon Aware Scheduler is a Kubernetes scheduler plugin that enables carbon and price-aware scheduling of pods based on real-time carbon intensity data and time-of-use electricity pricing.

## Features

- **Carbon-Aware Scheduling**: Schedule pods based on real-time carbon intensity data from Electricity Map API
- **Price-Aware Scheduling**: Schedule pods based on time-of-use electricity pricing schedules
- **Flexible Configuration**: Extensive configuration options for fine-tuning scheduler behavior
- **Pod-Level Controls**: Pods can opt-out or specify custom thresholds via annotations
- **Caching**: Built-in caching of API responses to limit external API calls
- **Observability**: Prometheus metrics for monitoring carbon intensity, pricing, and scheduling decisions

## Configuration

### Environment Variables

The scheduler can be configured using the following environment variables:

```bash
# API Configuration
ELECTRICITY_MAP_API_KEY=<your-api-key>  # Required: Your API key for Electricity Map API
ELECTRICITY_MAP_API_URL=<api-url>       # Optional: Default is https://api.electricitymap.org/v3/carbon-intensity/latest?zone=
ELECTRICITY_MAP_API_REGION=<region>     # Optional: Default is US-CAL-CISO
API_TIMEOUT=10s                         # Optional: API request timeout
API_MAX_RETRIES=3                       # Optional: Maximum API retry attempts
API_RETRY_DELAY=1s                      # Optional: Delay between retries
API_RATE_LIMIT=10                       # Optional: API rate limit per minute
CACHE_TTL=5m                           # Optional: Cache TTL for API responses
MAX_CACHE_AGE=1h                       # Optional: Maximum age of cached data

# Scheduling Configuration
CARBON_INTENSITY_THRESHOLD=200.0        # Optional: Base carbon intensity threshold (gCO2/kWh)
MAX_SCHEDULING_DELAY=24h               # Optional: Maximum pod scheduling delay
ENABLE_POD_PRIORITIES=false            # Optional: Enable pod priority-based scheduling

# Time-of-Use Pricing Configuration
PRICING_ENABLED=false                  # Optional: Enable TOU pricing
PRICING_PROVIDER=tou                   # Optional: Default is 'tou'
PRICING_BASE_RATE=0.10                 # Optional: Base electricity rate ($/kWh)
PRICING_PEAK_RATE=1.5                  # Optional: Peak rate multiplier
PRICING_MAX_DELAY=24h                   # Optional: Maximum delay for price-based scheduling
PRICING_SCHEDULES_PATH=/path/to/schedules.yaml  # Optional: Path to pricing schedules

# Observability Configuration
METRICS_ENABLED=true                   # Optional: Enable Prometheus metrics
METRICS_PORT=10259                     # Optional: Metrics server port
HEALTH_CHECK_ENABLED=true              # Optional: Enable health checks
HEALTH_CHECK_PORT=10258               # Optional: Health check server port
LOG_LEVEL=info                        # Optional: Logging level
ENABLE_TRACING=false                  # Optional: Enable tracing
```

### Time-of-Use Pricing Schedules

Time-of-use pricing schedules are defined in a YAML file:

```yaml
schedules:
  # Monday-Friday peak pricing periods (4pm-9pm)
  - dayOfWeek: "1-5"
    startTime: "16:00"
    endTime: "21:00"
  # Weekend peak pricing periods (1pm-7pm)
  - dayOfWeek: "0,6"
    startTime: "13:00"
    endTime: "19:00"
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
- `electricity_rate_gauge`: Current electricity rate based on TOU schedule
- `scheduling_attempts_total`: Total number of scheduling attempts by result
- `pod_scheduling_latency_seconds`: Pod scheduling latency histogram
- `carbon_savings_total`: Estimated carbon savings from delayed scheduling
- `cost_savings_total`: Estimated cost savings from delayed scheduling
- `price_based_delays_total`: Number of pods delayed due to pricing thresholds

## Architecture

The scheduler consists of several key components:

1. **Main Scheduler**: Implements the Kubernetes scheduler framework interfaces
2. **API Client**: Handles communication with Electricity Map API
3. **Cache**: Provides caching of API responses to reduce external API calls
4. **TOU Scheduler**: Manages time-of-use pricing schedules

### Scheduling Logic

The scheduler follows this decision flow:

1. Check if pod has exceeded maximum scheduling delay
2. Check for opt-out annotations
3. If pricing is enabled:
   - Get current rate from TOU schedule
   - Compare against threshold
4. Get current carbon intensity
5. Compare against threshold
6. Make scheduling decision

## Development

### Running Tests

```bash
# Run all tests
make test

# Run specific test
go test -v ./pkg/computegardener/... -run TestName

# Run tests with coverage
make test-coverage
```

### Adding a New Pricing Implementation

To add a new pricing implementation:

1. Create a new package under `pricing/`
2. Implement the `pricing.Implementation` interface
3. Add the implementation to the pricing factory
4. Add tests for the new implementation

## Contributing

Please see the [contributing guide](../../CONTRIBUTING.md) for guidelines on how to contribute to this project.
