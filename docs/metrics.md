# Metrics

Services built on core publish metrics by being scraped, not by pushing
OTLP. This page says what dashboards and alerts can rely on, and what a
service has to do to get there. It applies only to services that have
ported; a service still calling `SetupOpenTelemetry` pushes, and none of
the guarantees below hold for its pushed series.

## Contract for dashboards and alerts

Every series a ported service exposes on its metrics endpoint carries:

- `service`: the name of the service, e.g. `unikorn-region`. It is set
  in code, one value per service, the same for every binary in it
  (servers, controllers, monitors, consumers). Select a service with
  `service="unikorn-region"`.

The shared PodMonitor adds, as for any scraped pod:

- `namespace`, `pod` and `container`, to tell replicas and binaries apart.

Not part of the contract, so don't select on them:

- `job` and `instance`. The scraper sets them, and they differ between
  local Prometheus and the OTel target allocator.
- `app` and other pod labels. They come from the chart, often from the
  Helm release name.
- `service_name` on `target_info`. It is not set from the service name.

Points to be aware of:

- Series the scraper makes itself, such as `up` and `scrape_*`, do not
  carry `service`. Select those by `namespace` and `pod`.
- A metric with its own `service` label keeps it; core only adds the
  label where it is missing.
- Controller-runtime's `controller` label, and the workqueue's `name`
  label, are per controller binary, not per service.
- Traces use the same name: the trace resource's `service.name` is the
  service, so Tempo's span metrics have the same `service` value.
- OTel instrument names are stored as named, except that counters gain
  `_total`, and an instrument with a unit gains its unit suffix unless
  the name already ends with it.

## Porting a service

1. Depend on a core release that has `SetupMetrics(service)`.
2. Add the service name as a literal in the repo's `pkg/constants`, e.g.
   `Service = "unikorn-region"`. Don't derive it from the binary name or
   the environment. `constants.Application` stays as it is: it is also
   the leader-election lease ID and the `controller` label.
3. Controllers: replace `manager.Run(f)` with
   `manager.RunService(constants.Service, f)`. The manager already
   serves `/metrics` on `:8080`.
4. Other processes (API servers, monitors, consumers): replace
   `SetupOpenTelemetry(ctx)` with `SetupTracing(ctx, constants.Service)`
   and `SetupMetrics(constants.Service)`, called before any instrument is
   created. Then run `ServeMetrics(ctx, addr)`, on an address that
   doesn't clash with the API listener.
5. In the chart, every pod that serves metrics:
   - names its metrics container port `prometheus`, which the shared
     PodMonitor scrapes;
   - drops `OTEL_SERVICE_NAME`, which no longer has any effect.

   Remove any ServiceMonitor of the chart's own that scrapes the same
   port. It would double the series, and its `service` target label
   would rename ours to `exported_service`.
6. Check the service's namespace is in the shared PodMonitor
   (`k8s-deploy-unikorn`, `templates/monitoring/unikorn-monitors.yaml`).
7. Move dashboards and alerts that use the service's metrics onto the
   contract above. In particular, the Uni / API Health dashboard lists
   per-binary trace service names, which become a single service name.
   An alert that treats no data as OK goes quiet, without firing, if a
   query stops matching. Check those queries against the new labels
   before release.
8. After deploying, check `curl <pod>:<port>/metrics` shows
   `service="<name>"` on every series, and that
   `count by (service, pod) ({service="<name>"})` in central Mimir lists
   every pod of the service.
