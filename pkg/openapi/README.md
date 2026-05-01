# pkg/openapi

## Intention

`pkg/openapi` is the platform's shared OpenAPI substrate. It provides the common schema fragments, generated types, embedded specification loading, and runtime route lookup machinery that other service APIs build on.

Most of the package is generated from the shared OpenAPI specification. That generated layer defines reusable schema fragments, enums, generic error shapes, resource metadata models, and the embedded spec-loading functions used by downstream service-specific OpenAPI packages.

The handwritten [helpers](./helpers/README.md) layer is load-bearing runtime
infrastructure for the platform middleware stack. In particular, `FindRoute`
performs efficient path-based schema lookup from an already-routed Chi request,
resolves the matching OpenAPI operation, and returns both route metadata and
extracted path parameters.

That route metadata is then used to drive handler and middleware behavior from schema-defined data, including validation, authorization, CORS handling, auditing, and related request-processing decisions.

## Invariants And Guard Rails

- This package is shared OpenAPI contract infrastructure, not a generic HTTP utility package.
- The generated schema and types here are intended to be imported and reused by service-specific OpenAPI packages rather than redefined independently.
- The generated layer is the source of truth for the shared OpenAPI fragments it embeds. Manual edits belong in the specification or generation pipeline, not in generated files.
- `GetSwagger()` exposes the embedded shared specification, and `PathToRawSpec()` exists to support external reference resolution for downstream generated OpenAPI packages using import-mapped shared schema fragments.
- `helpers.FindRoute()` is part of the runtime contract of the platform middleware stack. It is not optional convenience code.
- Route lookup is intentionally coupled to requests that have already been routed by Chi. That is a performance choice: the platform reuses the routed path context for efficient schema lookup rather than paying for a second generic route-resolution pass.
- Middleware and handlers should derive schema-driven behavior from the resolved OpenAPI route rather than duplicate path logic separately.

## Caveats

- Most of this package is generated code, so the real design surface is smaller than the file count suggests.
- The handwritten helper layer is small, but it is operationally critical because middleware behavior depends on correct route-to-schema resolution.
- The package mixes static contract artifacts and runtime lookup logic. That is intentional for the platform, but it means the boundary is broader than "just generated types".
- The route lookup logic is coupled to Chi routing semantics and the platform's middleware ordering by design. Using it outside an already-routed Chi request path is the wrong usage pattern, not a generally supported mode.
