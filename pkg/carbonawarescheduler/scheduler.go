package carbonawarescheduler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/cache"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/config"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/peak"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/pricing"
)

const (
	// Name is the name of the plugin used in Registry and configurations.
	Name = "CarbonAwareScheduler"
)

// CarbonAwareScheduler is a scheduler plugin that implements carbon-aware scheduling
type CarbonAwareScheduler struct {
	handle framework.Handle
	config *config.Config

	// Components
	apiClient       *api.Client
	cache           *schedulercache.Cache
	peakScheduler   *peak.Scheduler
	pricingProvider pricing.Provider
	clock           clock.Clock

	// Shutdown
	stopCh chan struct{}
}

var (
	_ framework.PreFilterPlugin = &CarbonAwareScheduler{}
	_ framework.Plugin          = &CarbonAwareScheduler{}
)

// New initializes a new plugin and returns it
func New(ctx context.Context, obj runtime.Object, h framework.Handle) (framework.Plugin, error) {
	// Load and validate configuration
	cfg, err := config.Load(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}

	// Initialize components
	apiClient := api.NewClient(cfg.API)
	dataCache := schedulercache.New(cfg.API.CacheTTL, cfg.API.MaxCacheAge)
	peakScheduler := peak.New(cfg.PeakHours)

	// Initialize pricing provider if enabled
	var pricingProvider pricing.Provider
	if cfg.Pricing.Enabled {
		pricingCfg := &pricing.ProviderConfig{
			Enabled:    cfg.Pricing.Enabled,
			Provider:   cfg.Pricing.Provider,
			LocationID: cfg.Pricing.LocationID,
			APIKey:     cfg.Pricing.APIKey,
			MaxDelay:   cfg.Pricing.MaxDelay,
		}
		pricingCfg.Thresholds.Peak = cfg.Pricing.PeakThreshold
		pricingCfg.Thresholds.OffPeak = cfg.Pricing.OffPeakThreshold
		pricingProvider, err = pricing.NewProvider(pricingCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize pricing provider: %v", err)
		}
	}

	scheduler := &CarbonAwareScheduler{
		handle:          h,
		config:          cfg,
		apiClient:       apiClient,
		cache:           dataCache,
		peakScheduler:   peakScheduler,
		pricingProvider: pricingProvider,
		clock:           clock.RealClock{},
		stopCh:          make(chan struct{}),
	}

	// Start health check worker
	go scheduler.healthCheckWorker(ctx)

	// Register shutdown handler
	h.SharedInformerFactory().Core().V1().Nodes().Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
				klog.V(2).InfoS("Handling shutdown", "plugin", scheduler.Name())
				scheduler.Close()
			},
		},
	)

	// Start metrics server (insecure) on a separate mux
	go func() {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", legacyregistry.Handler())

		metricsServer := &http.Server{
			Addr:    ":2112",
			Handler: metricsMux,
		}

		klog.InfoS("Starting metrics server", "addr", ":2112")
		if err := metricsServer.ListenAndServe(); err != nil {
			klog.ErrorS(err, "Failed to start metrics server")
		}
	}()

	return scheduler, nil
}

// Name returns the name of the plugin
func (cs *CarbonAwareScheduler) Name() string {
	return Name
}

// PreFilter implements the PreFilter interface
func (cs *CarbonAwareScheduler) PreFilter(ctx context.Context, state *framework.CycleState, pod *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	startTime := cs.clock.Now()
	defer func() {
		PodSchedulingLatency.WithLabelValues("total").Observe(cs.clock.Since(startTime).Seconds())
	}()

	// Check if pod has been waiting too long
	if cs.hasExceededMaxDelay(pod) {
		SchedulingAttempts.WithLabelValues("max_delay_exceeded").Inc()
		return nil, framework.NewStatus(framework.Success, "maximum scheduling delay exceeded")
	}

	// Check if pod has annotation to opt-out
	if cs.isOptedOut(pod) {
		SchedulingAttempts.WithLabelValues("skipped").Inc()
		return nil, framework.NewStatus(framework.Success, "")
	}

	// Check pricing constraints if enabled
	if cs.config.Pricing.Enabled {
		if status := cs.checkPricingConstraints(ctx, pod); !status.IsSuccess() {
			return nil, status
		}
	}

	// Check carbon intensity constraints
	if status := cs.checkCarbonIntensityConstraints(ctx, pod); !status.IsSuccess() {
		return nil, status
	}

	return nil, framework.NewStatus(framework.Success, "")
}

// PreFilterExtensions returns nil as this plugin does not need extensions
func (cs *CarbonAwareScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// Close cleans up resources
func (cs *CarbonAwareScheduler) Close() error {
	close(cs.stopCh)
	cs.apiClient.Close()
	cs.cache.Close()
	return nil
}

func (cs *CarbonAwareScheduler) hasExceededMaxDelay(pod *v1.Pod) bool {
	if creationTime := pod.CreationTimestamp; !creationTime.IsZero() {
		return cs.clock.Since(creationTime.Time) > cs.config.Scheduling.MaxSchedulingDelay
	}
	return false
}

func (cs *CarbonAwareScheduler) isOptedOut(pod *v1.Pod) bool {
	return pod.Annotations["carbon-aware-scheduler.kubernetes.io/skip"] == "true" ||
		pod.Annotations["price-aware-scheduler.kubernetes.io/skip"] == "true"
}

func (cs *CarbonAwareScheduler) checkPricingConstraints(ctx context.Context, pod *v1.Pod) *framework.Status {
	isPeak, rate, err := cs.pricingProvider.IsPeakPeriod(ctx, cs.config.Pricing.LocationID)
	if err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("failed to get electricity price: %v", err))
	}

	// Record current electricity rate
	period := map[bool]string{true: "peak", false: "off-peak"}[isPeak]
	ElectricityRateGauge.WithLabelValues(cs.config.Pricing.LocationID, period).Set(rate)

	// Get threshold from pod annotation or use configured threshold
	threshold := cs.config.Pricing.OffPeakThreshold
	if isPeak {
		threshold = cs.config.Pricing.PeakThreshold
	}
	if val, ok := pod.Annotations["price-aware-scheduler.kubernetes.io/price-threshold"]; ok {
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
		} else {
			return framework.NewStatus(framework.Error, "invalid electricity price threshold annotation")
		}
	}

	if rate > threshold {
		PriceBasedDelays.WithLabelValues(period).Inc()
		savings := rate - threshold
		CostSavings.Add(savings)

		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current electricity rate ($%.3f/kWh) exceeds %s threshold ($%.3f/kWh)",
				rate,
				period,
				threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

func (cs *CarbonAwareScheduler) checkCarbonIntensityConstraints(ctx context.Context, pod *v1.Pod) *framework.Status {
	// Get carbon intensity data
	data, err := cs.getCarbonIntensityData(ctx)
	if err != nil {
		SchedulingAttempts.WithLabelValues("error").Inc()
		return framework.NewStatus(framework.Error, fmt.Sprintf("failed to get carbon intensity data: %v", err))
	}

	// Record carbon intensity metric
	CarbonIntensityGauge.WithLabelValues(cs.config.API.Region).Set(data.CarbonIntensity)

	// Get threshold from pod annotation or use configured threshold
	threshold := cs.config.Scheduling.BaseCarbonIntensityThreshold
	if val, ok := pod.Annotations["carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold"]; ok {
		if t, err := strconv.ParseFloat(val, 64); err == nil {
			threshold = t
		} else {
			return framework.NewStatus(framework.Error, "invalid carbon intensity threshold annotation")
		}
	}

	// Apply peak hour threshold if applicable
	threshold = cs.peakScheduler.GetCurrentThreshold(threshold, cs.clock.Now())

	if data.CarbonIntensity > threshold {
		SchedulingAttempts.WithLabelValues("intensity_exceeded").Inc()
		savings := data.CarbonIntensity - threshold
		CarbonSavings.Add(savings)

		return framework.NewStatus(
			framework.Unschedulable,
			fmt.Sprintf("Current carbon intensity (%.2f) exceeds threshold (%.2f)",
				data.CarbonIntensity,
				threshold),
		)
	}

	return framework.NewStatus(framework.Success, "")
}

func (cs *CarbonAwareScheduler) getCarbonIntensityData(ctx context.Context) (*api.ElectricityData, error) {
	// Check cache first
	if data, found := cs.cache.Get(cs.config.API.Region); found {
		return data, nil
	}

	// Fetch from API
	data, err := cs.apiClient.GetCarbonIntensity(ctx, cs.config.API.Region)
	if err != nil {
		return nil, err
	}

	// Update cache
	cs.cache.Set(cs.config.API.Region, data)
	return data, nil
}

func (cs *CarbonAwareScheduler) healthCheckWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cs.stopCh:
			return
		case <-ticker.C:
			if err := cs.healthCheck(ctx); err != nil {
				klog.ErrorS(err, "Health check failed")
			}
		}
	}
}

func (cs *CarbonAwareScheduler) healthCheck(ctx context.Context) error {
	_, err := cs.getCarbonIntensityData(ctx)
	return err
}
