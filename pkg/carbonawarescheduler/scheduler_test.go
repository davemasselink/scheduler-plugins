package carbonawarescheduler

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/api"
	schedulercache "sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/cache"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/clock"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/config"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/pricing/mock"
)

// testConfig wraps config.Config to implement runtime.Object
type testConfig struct {
	config.Config
}

func (c *testConfig) DeepCopyObject() runtime.Object {
	if c == nil {
		return nil
	}
	copy := *c
	return &copy
}

func (c *testConfig) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func setupTest(t *testing.T) func() {
	// Return a cleanup function
	return func() {
		// Clean up any test resources
		legacyregistry.Reset()
	}
}

func newTestScheduler(cfg *config.Config, carbonIntensity float64, rate float64, mockTime time.Time) *CarbonAwareScheduler {
	mockClient := api.NewClient(config.APIConfig{
		Key:       "mock-key",
		Region:    "mock-region",
		Timeout:   time.Second,
		RateLimit: 10,
		URL:       "http://mock-url/",
	})

	cache := schedulercache.New(time.Minute, time.Hour)
	cache.Set(cfg.API.Region, &api.ElectricityData{
		CarbonIntensity: carbonIntensity,
		Timestamp:       mockTime,
	})

	return &CarbonAwareScheduler{
		config:      cfg,
		apiClient:   mockClient,
		cache:       cache,
		pricingImpl: mock.New(rate),
		clock:       clock.NewMockClock(mockTime),
	}
}

func TestNew(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	tests := []struct {
		name    string
		obj     runtime.Object
		wantErr bool
	}{
		{
			name: "valid config",
			obj: &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						Key: "test-key",
					},
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: 200,
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "nil config",
			obj:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.obj, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPreFilter(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonIntensity float64
		threshold       float64
		electricityRate float64
		maxDelay        time.Duration
		podCreationTime time.Time
		wantStatus      *framework.Status
	}{
		{
			name: "pod should schedule - under threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should not schedule - over threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name: "pod should schedule - opted out",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
					Annotations: map[string]string{
						"carbon-aware-scheduler.kubernetes.io/skip": "true",
					},
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should schedule - max delay exceeded",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime.Add(-25 * time.Hour)),
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			maxDelay:        24 * time.Hour,
			podCreationTime: baseTime,
			wantStatus:      framework.NewStatus(framework.Success, "maximum scheduling delay exceeded"),
		},
		{
			name: "pod should not schedule - high electricity rate",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(baseTime),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			electricityRate: 0.20,
			podCreationTime: baseTime,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.200/kWh) exceeds threshold ($0.150/kWh)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						Key:    "test-key",
						Region: "test-region",
					},
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: tt.threshold,
						MaxSchedulingDelay:           tt.maxDelay,
					},
					Pricing: config.PricingConfig{
						Enabled:  true,
						Provider: "tou",
						Schedules: []config.Schedule{
							{
								DayOfWeek:   "0,1,2,3,4,5,6", // All days
								StartTime:   "00:00",
								EndTime:     "23:59",
								PeakRate:    0.25,
								OffPeakRate: 0.15, // This becomes default threshold
							},
						},
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, tt.electricityRate, tt.podCreationTime)

			result, status := scheduler.PreFilter(context.Background(), nil, tt.pod)
			if result != nil {
				t.Errorf("PreFilter() expected nil result, got %v", result)
			}
			if status.Code() != tt.wantStatus.Code() || status.Message() != tt.wantStatus.Message() {
				t.Errorf("PreFilter() status = %v, want %v", status, tt.wantStatus)
			}
		})
	}
}

func TestCheckPricingConstraints(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		pod        *v1.Pod
		rate       float64
		schedules  []config.Schedule
		wantStatus *framework.Status
	}{
		{
			name: "under off-peak rate",
			pod:  &v1.Pod{},
			rate: 0.12,
			schedules: []config.Schedule{
				{
					DayOfWeek:   "0,1,2,3,4,5,6",
					StartTime:   "00:00",
					EndTime:     "23:59",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
			wantStatus: framework.NewStatus(framework.Success, ""),
		},
		{
			name: "over off-peak rate",
			pod:  &v1.Pod{},
			rate: 0.18,
			schedules: []config.Schedule{
				{
					DayOfWeek:   "0,1,2,3,4,5,6",
					StartTime:   "00:00",
					EndTime:     "23:59",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.180/kWh) exceeds threshold ($0.150/kWh)",
			),
		},
		{
			name: "custom threshold from annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"price-aware-scheduler.kubernetes.io/price-threshold": "0.20",
					},
				},
			},
			rate: 0.18,
			schedules: []config.Schedule{
				{
					DayOfWeek:   "0,1,2,3,4,5,6",
					StartTime:   "00:00",
					EndTime:     "23:59",
					PeakRate:    0.25,
					OffPeakRate: 0.15,
				},
			},
			wantStatus: framework.NewStatus(framework.Success, ""),
		},
		{
			name:      "no schedules configured",
			pod:       &v1.Pod{},
			rate:      0.18,
			schedules: []config.Schedule{},
			wantStatus: framework.NewStatus(
				framework.Error,
				"no pricing schedules configured",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Pricing: config.PricingConfig{
						Enabled:   true,
						Provider:  "tou",
						Schedules: tt.schedules,
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, 0, tt.rate, baseTime)

			got := scheduler.checkPricingConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkPricingConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestCheckCarbonIntensityConstraints(t *testing.T) {
	cleanup := setupTest(t)
	defer cleanup()

	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonIntensity float64
		threshold       float64
		wantStatus      *framework.Status
	}{
		{
			name:            "under threshold",
			pod:             &v1.Pod{},
			carbonIntensity: 150,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name:            "over threshold",
			pod:             &v1.Pod{},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name: "custom threshold from annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold": "300",
					},
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						Key:    "test-key",
						Region: "test-region",
					},
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: tt.threshold,
					},
				},
			}

			scheduler := newTestScheduler(&cfg.Config, tt.carbonIntensity, 0, baseTime)

			got := scheduler.checkCarbonIntensityConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkCarbonIntensityConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}
