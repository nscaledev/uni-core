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
- `SetupOpenTelemetry()` is the common path for process-wide observability bootstrap. It sets global trace propagation plus tracer and meter providers for the process.
- When an OTLP endpoint is configured, `SetupOpenTelemetry()` also bridges controller-runtime Prometheus metrics into OTLP export rather than only enabling trace export.
- Helm chart helpers and values that surface these options are expected to stay aligned with the structs and flags defined here.

## Caveats

- The package name is broader than the actual scope. This is really shared runtime and bootstrap options, not a home for arbitrary application-specific settings.
- `CoreOptions` mixes several cross-cutting deployment concerns in one struct: namespace, logging, tracing, and metrics bootstrap. That is practical for shared process setup, but it is not a particularly clean abstraction boundary.
- The package registers flags and performs bootstrap wiring, but it does not validate higher-level application configuration or guarantee that callers use the options sensibly.
- `ServerOptions` only covers generic listener and timeout behavior. Service-specific server dependencies, middleware, auth, and peer-client options belong in higher-level packages that embed this base layer.
- The deployment contract is partly external to this package. If shared Helm helpers drift from these structs and flags, the shared process configuration contract is broken even if the Go code still compiles and starts.
