package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// GenabilityClient handles API communication with Genability
type GenabilityClient struct {
	apiKey     string
	apiURL     string
	httpClient *http.Client
	cache      *priceCache
	mutex      sync.RWMutex
}

type priceCache struct {
	data      *TOUPricing
	timestamp time.Time
}

// TOUPricing represents time-of-use electricity pricing data
type TOUPricing struct {
	CurrentRate float64
	PeakHours   []TOUPeriod
}

// TOUPeriod represents a time-of-use period with specific rates
type TOUPeriod struct {
	Name      string    // e.g., "On-Peak", "Off-Peak", "Super-Peak"
	StartTime time.Time // Start of the period
	EndTime   time.Time // End of the period
	Rate      float64   // Price per kWh
}

// NewGenabilityClient creates a new Genability API client
func NewGenabilityClient(apiKey string) *GenabilityClient {
	return &GenabilityClient{
		apiKey: apiKey,
		apiURL: "https://api.genability.com/v1",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: nil,
		mutex: sync.RWMutex{},
	}
}

// GetCurrentRate fetches the current electricity rate for the given tariff
func (c *GenabilityClient) GetCurrentRate(ctx context.Context, tariffID string) (float64, error) {
	// Check cache first
	c.mutex.RLock()
	if c.cache != nil {
		if time.Since(c.cache.timestamp) < 1*time.Hour { // Cache TOU data for 1 hour
			defer c.mutex.RUnlock()
			return c.cache.data.CurrentRate, nil
		}
	}
	c.mutex.RUnlock()

	// Cache miss or expired, fetch new data
	pricing, err := c.fetchTOUData(ctx, tariffID)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch TOU data: %v", err)
	}

	// Update cache
	c.mutex.Lock()
	c.cache = &priceCache{
		data:      pricing,
		timestamp: time.Now(),
	}
	c.mutex.Unlock()

	return pricing.CurrentRate, nil
}

// IsPeakPeriod checks if the current time is within a peak period
func (c *GenabilityClient) IsPeakPeriod(ctx context.Context, tariffID string) (bool, float64, error) {
	// Get current TOU data
	c.mutex.RLock()
	if c.cache == nil || time.Since(c.cache.timestamp) >= 1*time.Hour {
		c.mutex.RUnlock()
		if _, err := c.GetCurrentRate(ctx, tariffID); err != nil {
			return false, 0, err
		}
	} else {
		c.mutex.RUnlock()
	}

	c.mutex.RLock()
	defer c.mutex.RUnlock()

	now := time.Now()
	for _, period := range c.cache.data.PeakHours {
		if isTimeInPeriod(now, period.StartTime, period.EndTime) {
			return true, period.Rate, nil
		}
	}

	return false, c.cache.data.CurrentRate, nil
}

// Helper function to check if a time falls within a period
func isTimeInPeriod(t, start, end time.Time) bool {
	// Compare only hours and minutes
	timeStr := t.Format("15:04")
	startStr := start.Format("15:04")
	endStr := end.Format("15:04")

	return timeStr >= startStr && timeStr <= endStr
}

// fetchTOUData retrieves time-of-use pricing data from Genability
func (c *GenabilityClient) fetchTOUData(ctx context.Context, tariffID string) (*TOUPricing, error) {
	url := fmt.Sprintf("%s/tariffs/%s/prices", c.apiURL, tariffID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	// Parse response and extract TOU periods
	// Note: Actual response parsing will need to match Genability's API structure
	var pricing TOUPricing
	if err := json.NewDecoder(resp.Body).Decode(&pricing); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &pricing, nil
}
