# Carbon Aware Scheduler Manifests

This directory contains Kubernetes manifests for deploying the Carbon Aware Scheduler.

## Components

The deployment consists of several key components:

1. **ServiceAccount**: Required permissions for the scheduler
2. **RBAC**: Role bindings for scheduler permissions
3. **ConfigMap**: Configuration for the scheduler and peak hour schedules
4. **Deployment**: The scheduler deployment itself

## Configuration

### Scheduler Configuration

The scheduler configuration is managed through a ConfigMap (`carbon-aware-scheduler-config`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: carbon-aware-scheduler-config
  namespace: kube-system
data:
  carbon-aware-scheduler-config.yaml: |
    apiVersion: kubescheduler.config.k8s.io/v1
    kind: KubeSchedulerConfiguration
    profiles:
      - schedulerName: carbon-aware-scheduler
        plugins:
          preFilter:
            enabled:
              - name: CarbonAwareScheduler
    leaderElection:
      leaderElect: false
```

### Peak Hour Schedules

Peak hours are configured through a separate ConfigMap (`carbon-aware-peak-schedules`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: carbon-aware-peak-schedules
  namespace: kube-system
data:
  schedules.yaml: |
    # Peak schedule configuration using standard cron format
    # Format: day-of-week start-time end-time
    # day-of-week: 0-6 (Sunday=0)
    # time format: HH:MM in 24-hour format
    schedules:
      # Monday-Friday peak hours (typical 4pm-9pm peak period)
      - dayOfWeek: "1-5"
        startTime: "16:00"
        endTime: "21:00"
      # Weekend peak hours (example different schedule)
      - dayOfWeek: "0,6" 
        startTime: "13:00"
        endTime: "19:00"
```

### Environment Variables

The scheduler is configured through environment variables in the deployment:

- `ELECTRICITY_MAP_API_KEY`: API key for electricity data provider
- `CARBON_INTENSITY_THRESHOLD`: Base carbon intensity threshold (gCO2/kWh)
- `PEAK_CARBON_INTENSITY_THRESHOLD`: Carbon intensity threshold during peak hours
- `MAX_SCHEDULING_DELAY`: Maximum time to delay pod scheduling
- `PEAK_SCHEDULES_PATH`: Path to peak schedules configuration file

### Price-Aware Configuration

For price-aware scheduling, additional environment variables are required:

- `PRICING_ENABLED`: Enable price-aware scheduling ("true"/"false")
- `PRICING_PROVIDER`: Pricing data provider (e.g., "genability")
- `PRICING_API_KEY`: API key for the pricing provider
- `PRICING_LOCATION_ID`: Location identifier for pricing data
- `PRICING_PEAK_THRESHOLD`: Price threshold during peak hours ($/kWh)
- `PRICING_OFF_PEAK_THRESHOLD`: Price threshold during off-peak hours ($/kWh)
- `PRICING_MAX_DELAY`: Maximum delay for price-based scheduling

## Deployment

1. Create the required ConfigMaps:
```bash
kubectl apply -f carbon-aware-scheduler-config.yaml
kubectl apply -f carbon-aware-peak-schedules.yaml
```

2. Deploy the scheduler:
```bash
kubectl apply -f carbon-aware-scheduler.yaml
```

## Using the Scheduler

### Pod Configuration

To use the carbon-aware scheduler for a pod, set the scheduler name in the pod spec:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  schedulerName: carbon-aware-scheduler
  containers:
  - name: my-container
    image: my-image
```

### Pod Annotations

Pods can control scheduling behavior using annotations:

```yaml
metadata:
  annotations:
    # Opt out of carbon-aware scheduling
    carbon-aware-scheduler.kubernetes.io/skip: "true"
    
    # Set custom carbon intensity threshold
    carbon-aware-scheduler.kubernetes.io/carbon-intensity-threshold: "250.0"
    
    # Set custom price threshold
    price-aware-scheduler.kubernetes.io/price-threshold: "0.15"
```

## Monitoring

The scheduler exposes metrics on port 10259 for Prometheus scraping:

- `carbon_intensity_gauge`: Current carbon intensity
- `electricity_rate_gauge`: Current electricity rate
- `scheduling_attempts_total`: Scheduling attempt counts
- `pod_scheduling_latency_seconds`: Pod scheduling latency
- `carbon_savings_total`: Estimated carbon savings
- `cost_savings_total`: Estimated cost savings
- `price_based_delays_total`: Pricing-based delay counts

## Health Checks

The scheduler provides health checks on port 10259:
- Liveness: `/healthz`
- Readiness: `/healthz`

## Resource Requirements

The scheduler has minimal resource requirements:
```yaml
resources:
  requests:
    cpu: '0.1'
```

## Security Context

The scheduler runs with non-root privileges:
```yaml
securityContext:
  privileged: false
```

## Troubleshooting

Common issues and solutions:

1. **Scheduler not starting**: Check the scheduler logs:
```bash
kubectl logs -n kube-system -l component=scheduler
```

2. **Pods not scheduling**: Verify the pod's schedulerName matches:
```bash
kubectl get pod <pod-name> -o yaml | grep schedulerName
```

3. **API errors**: Check API key configuration:
```bash
kubectl get configmap -n kube-system carbon-aware-scheduler-config -o yaml
```

4. **Peak hours not working**: Verify peak schedules configuration:
```bash
kubectl get configmap -n kube-system carbon-aware-peak-schedules -o yaml
```

5. **Pricing issues**: Check pricing provider configuration:
```bash
# Check environment variables
kubectl get deployment -n kube-system carbon-aware-scheduler -o yaml | grep PRICING

# Check provider logs
kubectl logs -n kube-system -l component=scheduler | grep pricing
```
