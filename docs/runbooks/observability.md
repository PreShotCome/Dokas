# OpenTelemetry / Grafana — traces, metrics, logs

## What it powers

- **Traces** — every drill is one end-to-end trace (provision → fetch →
  restore → assert → report → teardown), and every HTTP request is a span.
- **Metrics** — Prometheus instruments (request rate/latency, drill
  outcomes, queue depth) exposed at `/metrics`.
- **Logs** — structured JSON to stdout, each line carrying `trace_id` and
  `account_id` for correlation.

## Code status — complete

- `internal/obs` — OpenTelemetry tracer provider, the Prometheus registry,
  and the slog logger are all wired. The trace exporter is config-gated:
  `OTEL_TRACES_EXPORTER` selects `otlp`, `stdout`, or none (no-op).
- Spans are tagged with `service.name=soteria` and the deployment
  environment, and trace context is propagated through River jobs.

## Setup

### Traces → an OTLP collector (Grafana Alloy, OpenTelemetry Collector, or
Grafana Cloud)

1. Stand up an OTLP/HTTP endpoint — self-hosted Grafana Alloy / OpenTelemetry
   Collector, or Grafana Cloud's OTLP gateway.
2. Set `OTEL_TRACES_EXPORTER=otlp`.
3. Point `OTEL_EXPORTER_OTLP_ENDPOINT` at the collector (e.g.
   `https://otlp-gateway-<region>.grafana.net/otlp`, or your collector's URL).
4. If the endpoint needs auth (Grafana Cloud does), set
   `OTEL_EXPORTER_OTLP_HEADERS` — e.g.
   `Authorization=Basic <base64(instanceID:token)>`.

These are the standard OpenTelemetry SDK variables; the exporter reads them
directly. Traces land in Tempo (or your tracing backend).

### Metrics → Prometheus

Scrape `GET /metrics`. If `METRICS_TOKEN` is set, the endpoint requires
`Authorization: Bearer <token>`; configure the scrape job's credentials to
match. Point Grafana at the Prometheus data source.

### Logs → Loki

The app logs structured JSON to stdout. Ship stdout to Loki with your
collector (Grafana Alloy tails container stdout). Logs join traces on
`trace_id`.

### Dashboards & alerts

Both are committed as IaC under `dashboards/`:

- `dashboards/soteria.json` — the service dashboard: HTTP request
  rate + latency, drill outcomes + duration, River queue depth, webhook
  deliveries.
- `dashboards/alerts.yml` — Prometheus alerting rules (app down, drill
  failure rate, queue backlog, 5xx rate, p95 latency, webhook failures),
  each linking the on-call incident runbook.
- `dashboards/provisioning/` — Grafana provisioning: a Prometheus
  datasource and a file-based dashboard provider. Mount
  `provisioning/datasources/` and `provisioning/dashboards/` into Grafana's
  `/etc/grafana/provisioning/`, mount `soteria.json` into
  `/etc/grafana/dashboards/`, and set `PROMETHEUS_URL` — Grafana then loads
  the dashboard on startup, no manual import. Load `alerts.yml` into
  Prometheus via `rule_files`.

### Environment variables

| Variable | Value |
|---|---|
| `OTEL_TRACES_EXPORTER` | `otlp` (prod), `stdout` (local debug), unset (off) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP/HTTP collector URL |
| `OTEL_EXPORTER_OTLP_HEADERS` | optional — auth headers for the endpoint |
| `METRICS_TOKEN` | optional — bearer token guarding `/metrics` |
| `PROMETHEUS_URL` | Prometheus URL, for the Grafana datasource provisioning |

## Verify

1. Locally, run with `OTEL_TRACES_EXPORTER=stdout` and trigger a drill — the
   six step spans print to the log as one trace tree.
2. In production with `otlp`, run a drill and confirm the trace appears in
   Tempo/Grafana under `service.name = soteria`.
3. `curl` `/metrics` (with the bearer token if `METRICS_TOKEN` is set) and
   confirm Prometheus is scraping it.
4. With the provisioning configs mounted, the "Soteria — Service"
   dashboard appears in Grafana on startup with no manual import.
