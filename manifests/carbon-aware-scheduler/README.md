# Carbon Aware Scheduler Manifests

This directory contains Kubernetes manifests for deploying the Carbon Aware Scheduler.

## Components

The deployment consists of several key components:

1. **ServiceAccount**: Required permissions for the scheduler
2. **RBAC**: Role bindings for scheduler permissions
3. **Secret**: API key for Electricity Map
4. **ConfigMaps**: Configuration for the scheduler and TOU pricing schedules
5. **Deployment**: The scheduler deployment itself

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

### Time-of-Use Pricing Schedules

Pricing schedules are configured through a ConfigMap (`carbon-aware-pricing-schedules`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: carbon-aware-pricing-schedules
  namespace: kube-system
data:
  schedules.yaml: |
    # Time-of-use pricing schedule configuration
    # Format: day-of-week start-time end-time
    # day-of-week: 0-6 (Sunday=0)
    # time format: HH:MM in 24-hour format
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

### API Key Configuration

The Electricity Map API key is stored in a Kubernetes secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: carbon-aware-scheduler-secrets
  namespace: kube-system
type: Opaque
data:
  electricity-map-api-key: <base64-encoded-api-key>
```

### Environment Variables

The scheduler is configured through environment variables in the deployment:

Carbon-Aware Configuration:
- `ELECTRICITY_MAP_API_KEY`: API key from secret (required)
- `CARBON_INTENSITY_THRESHOLD`: Base carbon intensity threshold (gCO2/kWh)
- `MAX_SCHEDULING_DELAY`: Maximum time to delay pod scheduling

Time-of-Use Pricing Configuration:
- `PRICING_ENABLED`: Enable price-aware scheduling ("true"/"false")
- `PRICING_PROVIDER`: Set to "tou" for time-of-use pricing
- `PRICING_BASE_RATE`: Base electricity rate ($/kWh)
- `PRICING_PEAK_RATE`: Peak rate multiplier (e.g., 1.5 for 50% higher)
- `PRICING_MAX_DELAY`: Maximum delay for price-based scheduling
- `PRICING_SCHEDULES_PATH`: Path to pricing schedules configuration file

## Deployment

1. Create the API key secret:
```bash
kubectl create secret generic carbon-aware-scheduler-secrets \
  --from-literal=electricity-map-api-key=YOUR_API_KEY \
  -n kube-system
```

2. Create the required ConfigMaps:
```bash
kubectl apply -f carbon-aware-scheduler-config.yaml
kubectl apply -f carbon-aware-pricing-schedules.yaml
```

3. Deploy the scheduler:
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
- `electricity_rate_gauge`: Current electricity rate based on TOU schedule
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

3. **API errors**: Check API key secret:
```bash
kubectl get secret -n kube-system carbon-aware-scheduler-secrets -o yaml
```

4. **TOU pricing not working**: Verify pricing schedules configuration:
```bash
kubectl get configmap -n kube-system carbon-aware-pricing-schedules -o yaml

# Check environment variables
kubectl get deployment -n kube-system carbon-aware-scheduler -o yaml | grep PRICING

# Check scheduler logs
kubectl logs -n kube-system -l component=scheduler | grep pricing
