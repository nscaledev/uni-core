# pkg/options

## Intention

`pkg/options` is the shared runtime-options package for platform processes. It does not provide a general configuration framework. Its job is to define the common CLI flag surface and bootstrap behavior that services, controllers, and consumers are expected to share for core process concerns.

This package is also part of the deployment contract, not just the Go API. The common flags defined here are expected to stay aligned with shared chart helpers so services expose a consistent runtime configuration surface through Helm as well as through code.

It currently provides two shared option groups:

- `CoreOptions` as the common process-bootstrap base layer for namespace, logging, and observability setup
- `ServerOptions` for standard HTTP listener and timeout configuration used by API processes

## Invariants And Guard Rails

- `CoreOptions` is the canonical base options layer for common process concerns. Higher-level service and controller option structs should embed or build on it rather than redefining namespace, logging, or OTLP flags locally.
- `ServerOptions` is the canonical shared flag surface for standard API server listener and timeout behavior.
- `AddFlags()` methods here define shared CLI contract. Changes to flag names, meanings, or defaults have operational impact beyond this package.
- `SetupLogging()` is the common path for wiring zap, controller-runtime logging, `klog`, and OpenTelemetry logging consistently in the same process.
- `SetupTracing(service)` sets global trace propagation and the tracer provider, exporting traces when an OTLP endpoint is configured. The trace resource's `service.name` is `service`, overriding `OTEL_SERVICE_NAME`.
- `SetupMetrics(service)` sets the global meter provider so OpenTelemetry instruments are exported on the controller-runtime registry and scraped with the controller-runtime metrics. Nothing is pushed. It replaces `ctrlmetrics.Registry` with a wrapper that adds `service="<service>"` (`ServiceLabel`) to every gathered series without one, so call it before the registry is served. Dashboards and alerts select a service by that label; it is the same for every binary in a service.
- Controller-runtime managers serve that registry at `:8080/metrics`. Processes without a manager call `ServeMetrics()` to serve it the same way, on an address of their choice.
- `SetupOpenTelemetry()` is deprecated. It calls `SetupTracing()`, then sets a meter provider that, when an OTLP endpoint is configured, pushes OpenTelemetry instruments and a copy of the controller-runtime metrics over OTLP. Pushed copies lack `namespace`, `pod` and `service` labels and duplicate the scraped series.
- A process installs one meter provider. Once `SetupMetrics()` or `SetupOpenTelemetry()` has done so, any further call to either fails, so neither replaces a meter provider whose instruments are already in use.
- Helm chart helpers and values that surface these options are expected to stay aligned with the structs and flags defined here.

## Caveats

- The package name is broader than the actual scope. This is really shared runtime and bootstrap options, not a home for arbitrary application-specific settings.
- `CoreOptions` mixes several cross-cutting deployment concerns in one struct: namespace, logging, tracing, and metrics bootstrap. That is practical for shared process setup, but it is not a particularly clean abstraction boundary.
- The package registers flags and performs bootstrap wiring, but it does not validate higher-level application configuration or guarantee that callers use the options sensibly.
- `ServerOptions` only covers generic listener and timeout behavior. Service-specific server dependencies, middleware, auth, and peer-client options belong in higher-level packages that embed this base layer.
- The deployment contract is partly external to this package. If shared Helm helpers drift from these structs and flags, the shared process configuration contract is broken even if the Go code still compiles and starts.
