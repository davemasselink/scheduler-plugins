package carbonawarescheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// cacheEntry represents a cached API response with timestamp
type cacheEntry struct {
	data      *ElectricityData
	timestamp time.Time
}

// PeakSchedule defines a time range for peak hours
type PeakSchedule struct {
	DayOfWeek string `yaml:"dayOfWeek"`
	StartTime string `yaml:"startTime"`
	EndTime   string `yaml:"endTime"`
}

// PeakSchedules contains a list of peak hour schedules
type PeakSchedules struct {
	Schedules []PeakSchedule `yaml:"schedules"`
}

// ValidateScheduleConfig validates a peak schedule configuration
func ValidateScheduleConfig(schedule PeakSchedule) error {
	// Validate day of week
	days := strings.Split(schedule.DayOfWeek, ",")
	for _, day := range days {
		dayRange := strings.Split(day, "-")
		if len(dayRange) > 2 {
			return fmt.Errorf("invalid day range format: %s", day)
		}
		for _, d := range dayRange {
			num, err := strconv.Atoi(d)
			if err != nil || num < 0 || num > 6 {
				return fmt.Errorf("invalid day of week: %s (must be 0-6)", d)
			}
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

// CarbonAwareScheduler is a scheduler plugin that implements carbon-aware scheduling.
// It tracks carbon intensity metrics and delays scheduling of pods during periods of high
// carbon intensity, helping to reduce the carbon footprint of workloads.
//
// The plugin supports:
// - Carbon intensity thresholds (base and peak hours)
// - Peak hour scheduling with configurable schedules
// - Maximum scheduling delay to ensure pods eventually run
// - Per-pod carbon intensity tracking and savings calculation
// - Prometheus metrics for carbon savings and scheduling decisions
//
// Configuration options:
// - ELECTRICITY_MAP_API_KEY: Required API key for carbon intensity data
// - CARBON_INTENSITY_THRESHOLD: Base threshold for carbon intensity (default: 150.0)
// - PEAK_CARBON_INTENSITY_THRESHOLD: Threshold during peak hours (default: 75% base)
// - MAX_SCHEDULING_DELAY: Maximum time to delay scheduling (default: 24h)
// - MAX_CONCURRENT_PODS: Maximum number of pods to schedule simultaneously (default: 2)
// - PEAK_SCHEDULES_PATH: Path to peak hours configuration file
// - ELECTRICITY_MAP_API_URL: Custom API endpoint (optional)
// - ELECTRICITY_MAP_API_REGION: Region for carbon intensity data (default: US-CAL-CISO)
//
// Pod annotations:
// - carbon-aware-scheduler.kubernetes.io/skip: Set to "true" to bypass carbon-aware scheduling
// - carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold: Override default threshold
// - carbon-aware-scheduler.kubernetes.io/initial-intensity: Set automatically to track savings
type CarbonAwareScheduler struct {
	handle                       framework.Handle
	apiKey                       string
	apiURL                       string
	apiRegion                    string
	cache                        *cacheEntry
	mutex                        sync.RWMutex
	peakSchedules                *PeakSchedules
	peakCarbonIntensityThreshold float64
	baseCarbonIntensityThreshold float64
	maxSchedulingDelay           time.Duration
	maxConcurrentPods            int
	currentlyScheduling          int
	stopCh                       chan struct{}
}

// ElectricityData represents the response from ElectricityMap API
type ElectricityData struct {
	CarbonIntensity float64 `json:"carbonIntensity"`
	// Add other fields as needed based on the API response
}

var (
	_ framework.PreFilterPlugin = &CarbonAwareScheduler{}
	_ framework.PostBindPlugin  = &CarbonAwareScheduler{}
	_ framework.Plugin          = &CarbonAwareScheduler{}
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "CarbonAwareScheduler"
)

var (
	// Default values that can be overridden by environment variables
	defaultBaseThreshold = 150.0
	defaultMaxDelay      = 24 * time.Hour
)

// Name returns the name of the plugin
func (es *CarbonAwareScheduler) Name() string {
	return Name
}

// New initializes a new plugin and returns it
func New(ctx context.Context, _ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	klog.V(1).Info("creating new carbon-aware-scheduler")

	// Get required API key
	apiKey := os.Getenv("ELECTRICITY_MAP_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ELECTRICITY_MAP_API_KEY environment variable is required")
	}

	// Get base carbon intensity threshold
	baseThreshold := defaultBaseThreshold
	if baseThresholdStr := os.Getenv("CARBON_INTENSITY_THRESHOLD"); baseThresholdStr != "" {
		if bt, err := strconv.ParseFloat(baseThresholdStr, 64); err == nil {
			baseThreshold = bt
		} else {
			return nil, fmt.Errorf("invalid CARBON_INTENSITY_THRESHOLD: %v", err)
		}
	}

	// Get max scheduling delay
	maxDelay := defaultMaxDelay
	if maxDelayStr := os.Getenv("MAX_SCHEDULING_DELAY"); maxDelayStr != "" {
		if md, err := time.ParseDuration(maxDelayStr); err == nil {
			maxDelay = md
		} else {
			return nil, fmt.Errorf("invalid MAX_SCHEDULING_DELAY (use format like '24h'): %v", err)
		}
	}

	// Get peak carbon intensity threshold
	peakThreshold := baseThreshold * 0.75 // Default to 75% of base threshold
	if peakThresholdStr := os.Getenv("PEAK_CARBON_INTENSITY_THRESHOLD"); peakThresholdStr != "" {
		if pt, err := strconv.ParseFloat(peakThresholdStr, 64); err == nil {
			peakThreshold = pt
		} else {
			return nil, fmt.Errorf("invalid PEAK_CARBON_INTENSITY_THRESHOLD: %v", err)
		}
	}

	// Load and validate peak schedules
	var peakSchedules *PeakSchedules
	if schedulePath := os.Getenv("PEAK_SCHEDULES_PATH"); schedulePath != "" {
		scheduleData, err := os.ReadFile(schedulePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read peak schedules: %v", err)
		}
		peakSchedules = &PeakSchedules{}
		if err := yaml.Unmarshal(scheduleData, peakSchedules); err != nil {
			return nil, fmt.Errorf("failed to parse peak schedules: %v", err)
		}

		// Validate each schedule
		for i, schedule := range peakSchedules.Schedules {
			if err := ValidateScheduleConfig(schedule); err != nil {
				return nil, fmt.Errorf("invalid schedule at index %d: %v", i, err)
			}
		}
		klog.V(2).InfoS("Loaded peak schedules configuration",
			"path", schedulePath,
			"scheduleCount", len(peakSchedules.Schedules))
	}

	apiURL := os.Getenv("ELECTRICITY_MAP_API_URL")
	if apiURL == "" {
		apiURL = "https://api.electricitymap.org/v3/carbon-intensity/latest?zone="
	}

	apiRegion := os.Getenv("ELECTRICITY_MAP_API_REGION")
	if apiRegion == "" {
		apiRegion = "US-CAL-CISO"
	}

	// Get max concurrent pods from env or use default
	maxConcurrentPods := 2 // Default to 2 concurrent pods
	if maxPodsStr := os.Getenv("MAX_CONCURRENT_PODS"); maxPodsStr != "" {
		if mp, err := strconv.Atoi(maxPodsStr); err == nil && mp > 0 {
			maxConcurrentPods = mp
		} else {
			return nil, fmt.Errorf("invalid MAX_CONCURRENT_PODS: %v", err)
		}
	}

	scheduler := &CarbonAwareScheduler{
		handle:                       h,
		apiKey:                       apiKey,
		apiURL:                       apiURL,
		apiRegion:                    apiRegion,
		cache:                        nil,
		mutex:                        sync.RWMutex{},
		peakSchedules:                peakSchedules,
		peakCarbonIntensityThreshold: peakThreshold,
		baseCarbonIntensityThreshold: baseThreshold,
		maxSchedulingDelay:           maxDelay,
		maxConcurrentPods:            maxConcurrentPods,
		currentlyScheduling:          0,
		stopCh:                       make(chan struct{}),
	}

	// Start health check worker
	go func() {
		for {
			select {
			case <-scheduler.stopCh:
				klog.V(2).InfoS("Stopping health check worker")
				return
			case <-time.After(30 * time.Second):
				if err := scheduler.Check(ctx); err != nil {
					klog.ErrorS(err, "Health check failed")
				}
			}
		}
	}()

	// Register shutdown handler
	h.SharedInformerFactory().Core().V1().Nodes().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
				klog.V(2).InfoS("Handling shutdown", "plugin", Name)
				scheduler.mutex.Lock()
				scheduler.cache = nil
				scheduler.mutex.Unlock()
			},
		},
	)

	return scheduler, nil
}

// Check performs a health check by verifying API connectivity
func (es *CarbonAwareScheduler) Check(ctx context.Context) error {
	// Perform API health check
	_, err := es.getElectricityData(ctx)
	if err != nil {
		klog.ErrorS(err, "Health check failed")
		return err
	}
	return nil
}

// Close cleans up the scheduler's resources
func (es *CarbonAwareScheduler) Close() error {
	klog.V(2).InfoS("Cleaning up carbon-aware-scheduler resources")

	// Signal health check worker to stop
	close(es.stopCh)

	// Clear cache
	es.mutex.Lock()
	es.cache = nil
	es.mutex.Unlock()

	return nil
}

// PreFilter implements the PreFilter interface
func (es *CarbonAwareScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	startTime := time.Now()
	defer func() {
		PodSchedulingLatency.WithLabelValues("total").Observe(time.Since(startTime).Seconds())
	}()

	// Check if pod has been waiting too long
	if creationTime := pod.CreationTimestamp; !creationTime.IsZero() {
		if time.Since(creationTime.Time) > es.maxSchedulingDelay {
			SchedulingAttempts.WithLabelValues("max_delay_exceeded").Inc()
			return nil, framework.NewStatus(framework.Success, "allowing pod due to maximum scheduling delay exceeded")
		}
	}

	// Check if pod has annotation to opt-out of carbon aware scheduling
	if pod.Annotations["carbon-aware-scheduler.kubernetes.io/skip"] == "true" {
		SchedulingAttempts.WithLabelValues("skipped").Inc()
		return nil, framework.NewStatus(framework.Success, "")
	}

	// Store initial carbon intensity if not already set
	if _, exists := pod.Annotations["carbon-aware-scheduler.kubernetes.io/initial-intensity"]; !exists {
		// Get electricity data from API to set initial intensity
		apiStartTime := time.Now()
		data, err := es.getElectricityData(ctx)
		if err != nil {
			SchedulingAttempts.WithLabelValues("error").Inc()
			PodSchedulingLatency.WithLabelValues("api_error").Observe(time.Since(apiStartTime).Seconds())
			return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get electricity data: %v", err))
		}
		PodSchedulingLatency.WithLabelValues("api_success").Observe(time.Since(apiStartTime).Seconds())

		// Store initial intensity as annotation
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations["carbon-aware-scheduler.kubernetes.io/initial-intensity"] = fmt.Sprintf("%.2f", data.CarbonIntensity)

		// Update the pod to persist the annotation
		if _, err := es.handle.ClientSet().CoreV1().Pods(pod.Namespace).Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
			klog.V(2).InfoS("Failed to update pod with initial carbon intensity",
				"error", err,
				"pod", klog.KObj(pod),
				"intensity", data.CarbonIntensity)
		} else {
			klog.V(4).InfoS("Updated pod with initial carbon intensity",
				"pod", klog.KObj(pod),
				"intensity", data.CarbonIntensity)
		}
	}

	// Get electricity data from API for current intensity
	apiStartTime := time.Now()
	data, err := es.getElectricityData(ctx)
	if err != nil {
		SchedulingAttempts.WithLabelValues("error").Inc()
		PodSchedulingLatency.WithLabelValues("api_error").Observe(time.Since(apiStartTime).Seconds())
		return nil, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get electricity data: %v", err))
	}
	PodSchedulingLatency.WithLabelValues("api_success").Observe(time.Since(apiStartTime).Seconds())

	// Record carbon intensity metric
	CarbonIntensityGauge.WithLabelValues(es.apiRegion).Set(data.CarbonIntensity)

	// Get threshold from pod annotation or use configured base threshold
	threshold := es.baseCarbonIntensityThreshold
	if val, ok := pod.Annotations["carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
		if _, err := fmt.Sscanf(val, "%f", &threshold); err != nil {
			SchedulingAttempts.WithLabelValues("invalid_threshold").Inc()
			return nil, framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation, accepts float")
		}
	}

	// Check if current time is within peak hours
	now := time.Now()
	currentThreshold := threshold
	isPeakHour := false

	if es.peakSchedules != nil {
		for _, schedule := range es.peakSchedules.Schedules {
			// Parse day of week ranges
			days := strings.Split(schedule.DayOfWeek, ",")
			for _, day := range days {
				dayRange := strings.Split(day, "-")
				start, _ := strconv.Atoi(dayRange[0])
				end := start
				if len(dayRange) > 1 {
					end, _ = strconv.Atoi(dayRange[1])
				}

				if int(now.Weekday()) >= start && int(now.Weekday()) <= end {
					// Parse time range
					startTime, _ := time.Parse("15:04", schedule.StartTime)
					endTime, _ := time.Parse("15:04", schedule.EndTime)

					// Compare only hours and minutes
					currentTime := now.Format("15:04")
					if currentTime >= startTime.Format("15:04") && currentTime <= endTime.Format("15:04") {
						currentThreshold = es.peakCarbonIntensityThreshold
						isPeakHour = true
						break
					}
				}
			}
			if isPeakHour {
				break
			}
		}
	}

	// Check if carbon intensity is above threshold
	if data.CarbonIntensity > currentThreshold {
		SchedulingAttempts.WithLabelValues("intensity_exceeded").Inc()
		// Calculate potential carbon savings
		savings := data.CarbonIntensity - currentThreshold
		CarbonSavings.Add(savings)

		thresholdType := "base"
		if isPeakHour {
			thresholdType = "peak"
		}
		return nil, framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current carbon intensity (%.2f) exceeds %s threshold (%.2f).",
				data.CarbonIntensity,
				thresholdType,
				currentThreshold,
			),
		)
	}

	// Check if we've hit max concurrent pods
	es.mutex.Lock()
	if es.currentlyScheduling >= es.maxConcurrentPods {
		es.mutex.Unlock()
		return nil, framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Max concurrent pods (%d) reached. Currently scheduling: %d pods",
				es.maxConcurrentPods,
				es.currentlyScheduling,
			),
		)
	}
	es.currentlyScheduling++
	es.mutex.Unlock()

	// Record successful region selection and update metrics
	RegionSelectionCount.WithLabelValues(es.apiRegion).Inc()
	SchedulingAttempts.WithLabelValues("success").Inc()

	// Calculate carbon savings if we have initial intensity
	if initialIntensityStr, exists := pod.Annotations["carbon-aware-scheduler.kubernetes.io/initial-intensity"]; exists {
		initialIntensity, err := strconv.ParseFloat(initialIntensityStr, 64)
		if err == nil && initialIntensity > data.CarbonIntensity {
			savings := initialIntensity - data.CarbonIntensity
			CarbonSavings.Add(savings)
			JobsScheduled.Inc()

			// Update average savings per job
			var jobMetric, savingsMetric dto.Metric
			if err := JobsScheduled.Write(&jobMetric); err != nil {
				klog.V(4).InfoS("Failed to read jobs scheduled metric",
					"error", err,
					"pod", klog.KObj(pod))
				return nil, framework.NewStatus(framework.Success, "")
			}
			if err := CarbonSavings.Write(&savingsMetric); err != nil {
				klog.V(4).InfoS("Failed to read carbon savings metric",
					"error", err,
					"pod", klog.KObj(pod))
				return nil, framework.NewStatus(framework.Success, "")
			}

			jobCount := jobMetric.Counter.GetValue()
			totalSavings := savingsMetric.Counter.GetValue()
			if jobCount > 0 {
				AverageCarbonSavingsPerJob.Set(totalSavings / jobCount)
				klog.V(4).InfoS("Updated carbon savings metrics",
					"pod", klog.KObj(pod),
					"totalSavings", totalSavings,
					"jobCount", jobCount,
					"averageSavings", totalSavings/jobCount)
			}
		}
	}

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (es *CarbonAwareScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// PostBind is called after a pod is successfully bound. Here we decrement our scheduling counter.
func (es *CarbonAwareScheduler) PostBind(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) {
	es.mutex.Lock()
	defer es.mutex.Unlock()
	if es.currentlyScheduling > 0 {
		es.currentlyScheduling--
		klog.V(4).InfoS("Decremented currently scheduling count after pod bind",
			"pod", klog.KObj(pod),
			"node", nodeName,
			"currentCount", es.currentlyScheduling)
	}
}

const (
	// CacheValidityDuration is how long the cache remains valid
	CacheValidityDuration = 5 * time.Minute
)

// getElectricityData fetches data from ElectricityMap API or returns cached data if valid
func (es *CarbonAwareScheduler) getElectricityData(ctx context.Context) (*ElectricityData, error) {
	// Check cache first
	es.mutex.RLock()
	if es.cache != nil {
		if time.Since(es.cache.timestamp) < CacheValidityDuration {
			defer es.mutex.RUnlock()
			return es.cache.data, nil
		}
	}
	es.mutex.RUnlock()

	// Cache miss or invalid, fetch new data
	klog.V(4).InfoS("Fetching carbon intensity data from API",
		"region", es.apiRegion,
		"url", es.apiURL)

	req, err := http.NewRequestWithContext(ctx, "GET", es.apiURL+es.apiRegion, nil)
	if err != nil {
		klog.V(2).InfoS("Failed to create API request",
			"error", err,
			"url", es.apiURL,
			"region", es.apiRegion)
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("auth-token", es.apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		klog.V(2).InfoS("Failed to make API request",
			"error", err,
			"url", es.apiURL,
			"region", es.apiRegion)
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		klog.V(2).InfoS("Received non-200 status code from API",
			"statusCode", resp.StatusCode,
			"url", es.apiURL,
			"region", es.apiRegion)
		return nil, fmt.Errorf("received non-200 status code: %d", resp.StatusCode)
	}

	var data ElectricityData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		klog.V(2).InfoS("Failed to decode API response",
			"error", err,
			"url", es.apiURL,
			"region", es.apiRegion)
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Update cache
	es.mutex.Lock()
	es.cache = &cacheEntry{
		data:      &data,
		timestamp: time.Now(),
	}
	es.mutex.Unlock()

	klog.V(4).InfoS("Successfully fetched and cached carbon intensity data",
		"region", es.apiRegion,
		"intensity", data.CarbonIntensity)

	return &data, nil
}
