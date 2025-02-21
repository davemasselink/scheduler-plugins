# Price-Aware Scheduling Provider

This package provides the interface and implementations for price-aware scheduling in the Carbon Aware Scheduler.

## Provider Interface

The pricing provider interface is defined in `provider.go`:

```go
type Provider interface {
    // GetCurrentRate returns the current electricity rate in $/kWh
    GetCurrentRate(ctx context.Context, locationID string) (float64, error)

    // IsPeakPeriod checks if the current time is within a peak pricing period
    // Returns: isPeak, currentRate, error
    IsPeakPeriod(ctx context.Context, locationID string) (bool, float64, error)
}
```

## Configuration

The provider configuration is defined in `config.go`:

```go
type ProviderConfig struct {
    Enabled    bool   `yaml:"enabled"`    // Enable price-aware scheduling
    Provider   string `yaml:"provider"`   // Provider name (e.g., "genability")
    LocationID string `yaml:"locationId"` // Location/utility identifier
    APIKey     string `yaml:"apiKey"`     // API authentication key
    MaxDelay   string `yaml:"maxDelay"`   // Maximum scheduling delay
    Thresholds struct {
        Peak    float64 `yaml:"peak"`    // Price threshold during peak hours
        OffPeak float64 `yaml:"offPeak"` // Price threshold during off-peak hours
    } `yaml:"thresholds"`
}
```

## Implementing a New Provider

To implement a new pricing provider:

1. Create a new file in the pricing package (e.g., `myprovider.go`)
2. Implement the Provider interface
3. Add provider initialization to the factory in `provider.go`

Example implementation:

```go
type MyProvider struct {
    config ProviderConfig
    client *http.Client
}

func NewMyProvider(config ProviderConfig) (*MyProvider, error) {
    return &MyProvider{
        config: config,
        client: &http.Client{
            Timeout: 10 * time.Second,
        },
    }, nil
}

func (p *MyProvider) GetCurrentRate(ctx context.Context, locationID string) (float64, error) {
    // Implement rate fetching logic
    return rate, nil
}

func (p *MyProvider) IsPeakPeriod(ctx context.Context, locationID string) (bool, float64, error) {
    // Implement peak period detection logic
    return isPeak, rate, nil
}
```

## Built-in Providers

### Genability Provider

The Genability provider (`genability.go`) integrates with the Genability API for real-time electricity pricing data:

- Supports multiple utility providers
- Real-time and historical pricing data
- Peak/off-peak period detection
- Rate caching for performance

Configuration:
```yaml
pricing:
  enabled: true
  provider: "genability"
  locationId: "utility-zone-1"
  apiKey: "your-api-key"
  maxDelay: "12h"
  thresholds:
    peak: 0.15    # $/kWh
    offPeak: 0.10 # $/kWh
```

### Error Handling

Providers should implement robust error handling:

1. **API Errors**: Handle and categorize API errors appropriately
2. **Rate Limiting**: Implement backoff/retry logic
3. **Caching**: Cache responses to handle API outages
4. **Validation**: Validate rates and thresholds

Example error handling:
```go
func (p *MyProvider) GetCurrentRate(ctx context.Context, locationID string) (float64, error) {
    // Check cache first
    if rate, found := p.cache.Get(locationID); found {
        return rate.(float64), nil
    }

    // Make API request with retry logic
    var rate float64
    err := retry.Do(func() error {
        var err error
        rate, err = p.fetchRate(ctx, locationID)
        return err
    }, retry.Attempts(3))

    if err != nil {
        // Return cached data if available
        if rate, found := p.cache.Get(locationID); found {
            return rate.(float64), nil
        }
        return 0, fmt.Errorf("failed to get rate: %v", err)
    }

    // Cache successful response
    p.cache.Set(locationID, rate, time.Minute*5)
    return rate, nil
}
```

## Best Practices

1. **Caching**
   - Cache API responses to reduce latency
   - Implement TTL-based cache invalidation
   - Use stale cache data during API outages

2. **Rate Limiting**
   - Respect API rate limits
   - Implement exponential backoff
   - Share rate limiters across instances

3. **Monitoring**
   - Export metrics for rate queries
   - Track cache hit rates
   - Monitor API errors

4. **Testing**
   - Mock API responses in tests
   - Test error handling paths
   - Validate rate thresholds

Example metrics:
```go
var (
    rateQueryTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "price_rate_queries_total",
            Help: "Total number of price rate queries",
        },
        []string{"provider", "result"},
    )

    rateQueryDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "price_rate_query_duration_seconds",
            Help: "Duration of price rate queries",
        },
        []string{"provider"},
    )
)
```

## Troubleshooting

Common issues and solutions:

1. **High Latency**
   - Check API response times
   - Verify cache configuration
   - Monitor rate limiting

2. **Invalid Rates**
   - Validate API responses
   - Check threshold configuration
   - Verify location IDs

3. **API Errors**
   - Check API credentials
   - Verify network connectivity
   - Monitor API status

4. **Cache Issues**
   - Check cache size/memory
   - Monitor cache hit rates
   - Verify TTL settings
