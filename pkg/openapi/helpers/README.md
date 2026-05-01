# pkg/openapi/helpers

## Intention

`pkg/openapi/helpers` is the runtime route-resolution layer for the platform's
OpenAPI-driven middleware stack. It is not a generic schema toolkit. Its job is to
take an already-routed HTTP request, map it efficiently onto the matching OpenAPI
path and operation, and return the route metadata that downstream middleware uses
to decide how the handler should behave.

This is the load-bearing runtime half of [pkg/openapi](../README.md). The
generated/spec side defines the schema. This package makes that schema usable on
live requests without doing an expensive second routing pass.

## What Lives Here

- `Schema`, a thin cached wrapper around a loaded `openapi3.T`.
- `NewSchema()`, which loads and retains the parsed specification.
- `FindRoute()`, which uses Chi's existing route context to resolve the matching
  OpenAPI path, operation, and path parameters.

## Relationships

- [pkg/openapi](../README.md) owns the shared generated schema/types and embedded
  spec-loading functions.
- [pkg/server/middleware](../../server/middleware/README.md), especially
  `routeresolver` and `cors`, depends on this package to attach route metadata to
  request context once and reuse it downstream.
- [pkg/server/errors](../../server/errors/README.md) provides the canonical API
  errors returned when route or method lookup fails.

## Invariants

- Requests are expected to have already passed through Chi routing. `FindRoute()`
  does not perform a generic independent router/spec match. It reuses the routed
  path context deliberately for performance.
- Middleware ordering is therefore part of the contract. Using this package before
  routing, or outside the standard request pipeline, is the wrong execution model.
- `Schema` should be constructed once and reused. Parsing the OpenAPI document is
  intentionally separated from per-request route resolution because loading the
  spec repeatedly is unnecessarily expensive.

## Caveats

- This package is tightly coupled to Chi. That is a deliberate performance tradeoff,
  not an accidental implementation detail.
- `FindRoute()` assumes the routed path exists in the OpenAPI specification too.
  If router definitions and schema drift apart, this package turns that drift into
  runtime 404/405-style API errors.
