# pkg/server/errors

## Intention

`pkg/server/errors` defines the canonical user-facing error surface for the platform's APIs. It is the core error type that handlers and middleware use to turn failures into the structured JSON error responses returned to clients on almost every API call.

The wire format is inspired by OAuth2 error semantics, but it is not limited to OAuth2 endpoints. The platform extends that model so the same terse-code-plus-description shape can be used consistently across other APIs as well.

This package combines three closely related responsibilities:

- constructing platform API errors with HTTP status, terse error code, user-facing description, optional headers, and internal logging detail
- writing those errors to HTTP responses, including the trace ID that clients can use when reporting failures
- propagating and normalizing API errors across service-to-service OpenAPI calls so one service can surface another service's failure using the same local error model

OAuth2 endpoints are the partial exception rather than the opposite. They still use this package heavily, but some of their wire semantics are constrained by the formal OAuth2 and related specifications rather than only by the platform's broader API conventions.

## Invariants And Guard Rails

- This is the canonical platform API error type for normal APIs, not just an internal helper.
- Errors here are user-facing contract objects. Their HTTP status, terse code, description, and header behavior are part of the API surface clients observe.
- The error shape is OAuth2-inspired but intentionally reusable across non-OAuth2 APIs.
- `WithError()` and `WithValues()` are for internal logging context. They augment server-side observability and must not be treated as additional client-visible payload.
- `Write()` is responsible for emitting the standard JSON error body and, when trace context is present, the trace ID clients use for support correlation.
- Constructors such as `HTTPNotFound`, `HTTPConflict`, `OAuth2InvalidRequest`, `AccessDenied`, and related helpers are the standard way to create common API failure classes.
- `HandleError()` is the main normalization point for handlers and middleware that need to surface arbitrary failures through the platform error contract.
- `PropagateError()` is the main cross-service adapter for generated OpenAPI client response types.
- `FromOpenAPIError()` is the narrower helper for paths that already hold a decoded `openapi.Error` payload and need to rebuild the local error model from it.

## Caveats

- The package is the platform's generalized OAuth2-inspired error model, while pure OAuth2 and related authentication flows remain the constrained special case where formal RFC wire semantics still apply.
- `PropagateError()` is a pragmatic workaround for how `oapi-codegen` generated `*WithResponse` client types expose per-status response payloads through fields such as `JSON400`, `JSON404`, and similar. It relies on reflection because the generator does not provide a cleaner typed error path.
- The package mixes user-facing wire contract, logging policy, header encoding, and service-to-service error propagation in one place. That is practical, but it makes the boundary broader than a pure response-type package.
- If an error is created or propagated poorly, the package will still emit a client response, but the quality of support/debugging information depends heavily on callers attaching useful internal context with `WithError()` and `WithValues()`.
