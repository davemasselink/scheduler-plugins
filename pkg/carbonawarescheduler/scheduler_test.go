package carbonawarescheduler

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/api"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/config"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/peak"
	"sigs.k8s.io/scheduler-plugins/pkg/carbonawarescheduler/pricing"
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

// mockAPIClient implements api.Client interface
type mockAPIClient struct {
	carbonIntensity float64
	err             error
}

func NewMockAPIClient(carbonIntensity float64, err error) *api.Client {
	baseClient := api.NewClient(config.APIConfig{
		Provider:   "mock",
		Key:        "mock-key",
		Region:     "mock-region",
		Timeout:    time.Second,
		RateLimit:  10,
		MaxRetries: 1,
	})
	return baseClient
}

func (m *mockAPIClient) GetCarbonIntensity(ctx context.Context, region string) (*api.ElectricityData, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &api.ElectricityData{
		CarbonIntensity: m.carbonIntensity,
		Timestamp:       time.Now(),
	}, nil
}

func (m *mockAPIClient) Close() {}

type mockPricingProvider struct {
	pricing.Provider
	isPeak bool
	rate   float64
	err    error
}

func (m *mockPricingProvider) GetCurrentRate(ctx context.Context, locationID string) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.rate, nil
}

func (m *mockPricingProvider) IsPeakPeriod(ctx context.Context, locationID string) (bool, float64, error) {
	if m.err != nil {
		return false, 0, m.err
	}
	return m.isPeak, m.rate, nil
}

func TestNew(t *testing.T) {
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
						Provider: "test",
						Key:      "test-key",
						Region:   "test-region",
					},
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: 200,
						MaxConcurrentPods:            10,
					},
				},
			},
			wantErr: false,
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
	tests := []struct {
		name            string
		pod             *v1.Pod
		carbonIntensity float64
		threshold       float64
		isPeak          bool
		electricityRate float64
		priceThreshold  float64
		maxDelay        time.Duration
		wantStatus      *framework.Status
	}{
		{
			name: "pod should schedule - under threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should not schedule - over threshold",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current carbon intensity (250.00) exceeds threshold (200.00)",
			),
		},
		{
			name: "pod should schedule - opted out",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
					Annotations: map[string]string{
						"carbon-aware-scheduler.kubernetes.io/skip": "true",
					},
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			wantStatus:      framework.NewStatus(framework.Success, ""),
		},
		{
			name: "pod should schedule - max delay exceeded",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.NewTime(time.Now().Add(-25 * time.Hour)),
				},
			},
			carbonIntensity: 250,
			threshold:       200,
			maxDelay:        24 * time.Hour,
			wantStatus:      framework.NewStatus(framework.Success, "maximum scheduling delay exceeded"),
		},
		{
			name: "pod should not schedule - peak pricing",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
				},
			},
			carbonIntensity: 150,
			threshold:       200,
			isPeak:          true,
			electricityRate: 0.20,
			priceThreshold:  0.15,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.200/kWh) exceeds peak threshold ($0.150/kWh)",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					API: config.APIConfig{
						Provider: "test",
						Key:      "test-key",
						Region:   "test-region",
					},
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: tt.threshold,
						MaxSchedulingDelay:           tt.maxDelay,
						MaxConcurrentPods:            10,
					},
					Pricing: config.PricingConfig{
						Enabled:          true,
						PeakThreshold:    tt.priceThreshold,
						OffPeakThreshold: tt.priceThreshold,
					},
				},
			}

			mockClient := NewMockAPIClient(tt.carbonIntensity, nil)

			scheduler := &CarbonAwareScheduler{
				config:        &cfg.Config,
				apiClient:     mockClient,
				peakScheduler: peak.New(config.PeakHoursConfig{}),
				pricingProvider: &mockPricingProvider{
					isPeak: tt.isPeak,
					rate:   tt.electricityRate,
				},
			}

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
	tests := []struct {
		name             string
		pod              *v1.Pod
		isPeak           bool
		rate             float64
		peakThreshold    float64
		offPeakThreshold float64
		wantStatus       *framework.Status
	}{
		{
			name:          "under peak threshold",
			pod:           &v1.Pod{},
			isPeak:        true,
			rate:          0.12,
			peakThreshold: 0.15,
			wantStatus:    framework.NewStatus(framework.Success, ""),
		},
		{
			name:          "over peak threshold",
			pod:           &v1.Pod{},
			isPeak:        true,
			rate:          0.18,
			peakThreshold: 0.15,
			wantStatus: framework.NewStatus(
				framework.Unschedulable,
				"Current electricity rate ($0.180/kWh) exceeds peak threshold ($0.150/kWh)",
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
			isPeak:        true,
			rate:          0.18,
			peakThreshold: 0.15,
			wantStatus:    framework.NewStatus(framework.Success, ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &testConfig{
				Config: config.Config{
					Pricing: config.PricingConfig{
						Enabled:          true,
						PeakThreshold:    tt.peakThreshold,
						OffPeakThreshold: tt.offPeakThreshold,
					},
				},
			}

			scheduler := &CarbonAwareScheduler{
				config: &cfg.Config,
				pricingProvider: &mockPricingProvider{
					isPeak: tt.isPeak,
					rate:   tt.rate,
				},
			}

			got := scheduler.checkPricingConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkPricingConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}

func TestCheckCarbonIntensityConstraints(t *testing.T) {
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
					Scheduling: config.SchedulingConfig{
						BaseCarbonIntensityThreshold: tt.threshold,
					},
				},
			}

			mockClient := NewMockAPIClient(tt.carbonIntensity, nil)

			scheduler := &CarbonAwareScheduler{
				config:        &cfg.Config,
				apiClient:     mockClient,
				peakScheduler: peak.New(config.PeakHoursConfig{}),
			}

			got := scheduler.checkCarbonIntensityConstraints(context.Background(), tt.pod)
			if got.Code() != tt.wantStatus.Code() || got.Message() != tt.wantStatus.Message() {
				t.Errorf("checkCarbonIntensityConstraints() = %v, want %v", got, tt.wantStatus)
			}
		})
	}
}
