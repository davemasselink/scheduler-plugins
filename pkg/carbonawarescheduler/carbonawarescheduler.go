package carbonawarescheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// CarbonAwareScheduler is a scheduler plugin that checks electricity data
type CarbonAwareScheduler struct {
	handle framework.Handle
	apiKey string
	apiURL string
}

// ElectricityData represents the response from ElectricityMap API
type ElectricityData struct {
	CarbonIntensity float64 `json:"carbonIntensity"`
	// Add other fields as needed based on the API response
}

var _ framework.PreFilterPlugin = &CarbonAwareScheduler{}

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name               = "CarbonAwareScheduler"
	DefaultThreshold   = 200.0
	MaxSchedulingDelay = 24 * time.Hour
)

// Name returns the name of the plugin
func (es *CarbonAwareScheduler) Name() string {
	return Name
}

// New initializes a new plugin and returns it
func New(ctx context.Context, _ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	klog.V(1).Info("creating new carbon-aware-scheduler")

	apiKey := os.Getenv("ELECTRICITY_MAP_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELECTRICITY_MAP_API_KEY environment variable is required")
	}

	apiURL := os.Getenv("ELECTRICITY_MAP_API_URL")
	if apiURL == "" {
		apiURL = "https://api.electricitymap.org/v3/carbon-intensity/latest?zone=US-CAL-CISO"
	}

	return &CarbonAwareScheduler{
		handle: h,
		apiKey: apiKey,
		apiURL: apiURL,
	}, nil
}

// PreFilter implements the PreFilter interface
func (es *CarbonAwareScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	// Check if pod has been waiting too long (hard-coded to 24 hrs)
	if creationTime := pod.CreationTimestamp; !creationTime.IsZero() {
		if time.Since(creationTime.Time) > MaxSchedulingDelay {
			return nil, framework.NewStatus(framework.Success,
				"allowing pod due to maximum scheduling delay exceeded")
		}
	}

	// Check if pod has annotation to opt-out of carbon aware scheduling
	if pod.Annotations["carbon-aware-scheduler.kubernetes.io/skip"] == "true" {
		return nil, framework.NewStatus(framework.Success, "")
	}

	// Get electricity data from API
	data, err := es.getElectricityData(ctx)
	if err != nil {
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get electricity data: %v", err))
	}

	// Get threshold from pod annotation or use default
	threshold := DefaultThreshold
	if val, ok := pod.Annotations["carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
		if _, err := fmt.Sscanf(val, "%f", &threshold); err != nil {
			return nil, framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation, accepts float")
		}
	}

	// Check if carbon intensity is above threshold
	if data.CarbonIntensity > threshold {
		return nil, framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f).",
				data.CarbonIntensity,
				threshold,
			),
		)
	}

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (es *CarbonAwareScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// getElectricityData fetches data from ElectricityMap API
func (es *CarbonAwareScheduler) getElectricityData(ctx context.Context) (*ElectricityData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", es.apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("auth-token", es.apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	var data ElectricityData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &data, nil
}
