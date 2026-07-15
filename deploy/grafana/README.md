# Grafana dashboard

`dashboard.json` visualises the Prometheus metrics exported by dps150-web at
`GET /metrics` — live voltage/current/power/temperature, measured-vs-setpoint,
charge/energy counters, active protection, command latency and link health.

## Import

Grafana → **Dashboards → New → Import** → upload `dashboard.json` (or paste it),
then pick your Prometheus data source when prompted. The dashboard UID is
`dps150-web`.

## Prerequisite: scrape the backend

The backend must be scraped by Prometheus. Point a scrape job at the backend's
`/metrics`, e.g. a plain job:

```yaml
scrape_configs:
  - job_name: dps150-web
    static_configs:
      - targets: ["dps150-backend:8080"]
```

On Kubernetes with the Prometheus Operator, use a `ServiceMonitor`/`PodMonitor`
selecting the backend Service instead.

## Provisioning (optional)

To ship the dashboard as code, drop `dashboard.json` into a Grafana dashboard
provider path (a `dashboardproviders` folder, or a `ConfigMap` labelled for the
Grafana sidecar), and Grafana loads it on start.
