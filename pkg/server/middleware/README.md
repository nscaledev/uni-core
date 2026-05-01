# pkg/server/middleware

## Intention

`pkg/server/middleware` provides the canonical shared middleware stack for platform APIs. It contains the common request-pipeline components that platform services are expected to compose before adding any service-specific middleware of their own.

The subpackages are cooperative and order-sensitive rather than interchangeable. They establish trace context and correlated logging context, resolve OpenAPI route metadata, implement schema-driven CORS behavior, and add request-context timeouts. The root package also provides generic response-capture helpers used by tests and by middleware patterns that need to inspect responses.

This package does not try to own every middleware concern in the platform. Service-specific middleware still belongs with the service that owns the behavior, for example identity-specific authentication and authorization layers.

For the OpenAPI route-resolution contract that this stack depends on, see [pkg/openapi/README.md](/home/simon/src/github.com/unikorn-cloud/core/pkg/openapi/README.md).

## Invariants And Guard Rails

- This is the canonical shared middleware stack for platform APIs, not a miscellaneous collection of unrelated HTTP helpers.
- Middleware in this directory is designed to cooperate as a request pipeline. Ordering is part of the contract.
- `opentelemetry` must establish trace context early because the trace ID is a customer-facing correlation handle for failures and a primary way to connect support requests to logs and telemetry.
- `logging` depends on request context and response metrics to produce useful request and response records without exposing obviously sensitive headers.
- `routeresolver` is load-bearing shared middleware. It resolves OpenAPI route metadata once and stashes it in context for downstream consumers. See [pkg/openapi/README.md](/home/simon/src/github.com/unikorn-cloud/core/pkg/openapi/README.md).
- `cors` depends on that resolved route information, especially for emulated `OPTIONS` handling.
- `timeout` adds request-context deadlines. Downstream handlers and middleware must respect context cancellation for it to be effective.
- Service packages may add their own middleware, but domain-specific concerns should live with the package that owns the behavior rather than being pushed into this shared stack.

## Caveats

- The root package boundary is slightly awkward: `Capture` is generic response-capture infrastructure, while most of the real behavior lives in subpackages.
- Middleware ordering is not optional. Reordering pieces such as route resolution and CORS can change behavior or break schema-driven handling.
- `timeout` is intentionally simple context wrapping, not a full response-timeout or request-abort framework. Work that ignores context can outlive the intended deadline.
- The canonical shared stack is not exhaustive. Service-specific packages will still define additional middleware where the behavior is not platform-generic.
