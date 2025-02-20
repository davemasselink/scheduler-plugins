package carbonawarescheduler

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	// Subsystem name used for scheduler metrics
	schedulerSubsystem = "scheduler_carbon_aware"
)

var (
	// CarbonIntensityGauge measures the current carbon intensity for a region
	CarbonIntensityGauge = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "carbon_intensity",
			Help:           "Current carbon intensity (gCO2eq/kWh) for a given region",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"region"},
	)

	// PodSchedulingLatency measures the latency of pod scheduling attempts
	PodSchedulingLatency = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "pod_scheduling_duration_seconds",
			Help:           "Latency for scheduling attempts in the carbon-aware scheduler",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"result"}, // "total", "api_success", "api_error"
	)

	// RegionSelectionCount counts the number of times each region was selected for scheduling
	RegionSelectionCount = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "region_selection_total",
			Help:           "Number of times a region was selected for scheduling",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"region"},
	)

	// SchedulingAttempts counts the total number of scheduling attempts
	SchedulingAttempts = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "scheduling_attempt_total",
			Help:           "Number of attempts to schedule pods by result",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"result"}, // "success", "error", "skipped", "max_delay_exceeded", "invalid_threshold", "intensity_exceeded"
	)

	// CarbonSavings estimates the carbon emissions saved through carbon-aware scheduling
	CarbonSavings = metrics.NewCounter(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "carbon_savings_grams",
			Help:           "Estimated carbon emissions saved (in grams of CO2) through carbon-aware scheduling",
			StabilityLevel: metrics.ALPHA,
		},
	)

	// JobsScheduled counts the total number of jobs that were carbon-aware scheduled
	JobsScheduled = metrics.NewCounter(
		&metrics.CounterOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "jobs_scheduled_total",
			Help:           "Total number of jobs that were carbon-aware scheduled",
			StabilityLevel: metrics.ALPHA,
		},
	)

	// AverageCarbonSavingsPerJob tracks the average carbon savings per scheduled job
	AverageCarbonSavingsPerJob = metrics.NewGauge(
		&metrics.GaugeOpts{
			Subsystem:      schedulerSubsystem,
			Name:           "average_carbon_savings_per_job_grams",
			Help:           "Average carbon emissions saved per job (in grams of CO2)",
			StabilityLevel: metrics.ALPHA,
		},
	)
)

func init() {
	// Register all metrics with the legacy registry
	legacyregistry.MustRegister(CarbonIntensityGauge)
	legacyregistry.MustRegister(PodSchedulingLatency)
	legacyregistry.MustRegister(RegionSelectionCount)
	legacyregistry.MustRegister(SchedulingAttempts)
	legacyregistry.MustRegister(CarbonSavings)
	legacyregistry.MustRegister(JobsScheduled)
	legacyregistry.MustRegister(AverageCarbonSavingsPerJob)
}
