# pkg/server

## Intention

`pkg/server` is the shared API-server support layer for platform services. It is not a complete server framework by itself, but it collects the common building blocks that turn generated OpenAPI handlers into the platform's standard HTTP behavior.

At the top level, this directory brings together:

- API-boundary conversion between Kubernetes resources and shared API envelopes
- the canonical user-facing error surface for normal APIs
- the canonical shared middleware stack for request processing
- a first-cut in-process saga helper for best-effort multi-step rollback
- a small set of handler-adjacent utility functions

This package family is tightly connected to the shared OpenAPI substrate documented in [pkg/openapi](../openapi/README.md), because route resolution, schema-driven middleware behavior, and common error responses all depend on that contract.

## Resource ID Allocation Strategy

All Kubernetes resource names in this platform are UUIDs. There are two strategies and the choice is architectural, not cosmetic:

- **Random UUID** (`NewObjectMetadata`): use for resources whose identity has no natural key — the platform is the sole authority on what the name is. This is the default.
- **Deterministic UUID** (`NewDeterministicObjectMetadata`): use when the resource participates in a uniqueness contract derived from its content — for example, a hostname on a virtual network must be unique within that network, so the name is derived from `(network-id, hostname)`. Two creates with the same invariant produce the same Kubernetes name and the second is rejected with a 409, giving conflict detection without a read-before-write.

The rule of thumb: if a duplicate of this resource would be silently wrong — not just redundant but semantically broken (name collision on a network, duplicate route entry, etc.) — use deterministic IDs. Otherwise use random IDs.

## Invariants And Guard Rails

- `pkg/server` is for common API-server behavior across services, not for service-specific handler logic, auth policy, or domain-owned middleware.
- The shared pieces here are intended to preserve one platform-wide API experience:
  - common request/response shaping
  - common route/schema interpretation
  - common middleware ordering expectations
  - common user-facing error structure
- Service packages are expected to build on these lower layers rather than reimplementing them ad hoc, while still adding their own domain-specific handlers and middleware where appropriate.
- The top-level story here is cooperative composition, not a single monolithic abstraction. The subpackages are the real units of behavior.

## Package Map

- [conversion](./conversion/README.md): shared generic conversion layer for common resource metadata, status, and tag translation between Kubernetes objects and API envelopes.
- [errors](./errors/README.md): canonical user-facing API error contract and response writer for normal APIs, plus propagation helpers for remote API failures.
- [middleware](./middleware/README.md): canonical shared middleware stack for platform APIs, including route resolution, CORS, logging, tracing, timeout, and response capture support.
- [saga](./saga/README.md): synchronous in-process best-effort rollback coordinator for multi-step handler workflows.
- [util](./util/README.md): small server-boundary helper bucket for response writing, request-body decoding, ownership concealment checks, and tag parsing.

## Caveats

- `pkg/server` is still a family of helpers rather than a clean, unified server framework. The subpackages are related by usage in service APIs more than by one elegant abstraction boundary.
- Some of the most important behavior here is context- and ordering-sensitive, especially in [middleware](./middleware/README.md) and [errors](./errors/README.md).
- The server layer depends heavily on the shared OpenAPI/runtime contract. If route generation or schema usage changes materially, several subpackages here would need to move in lock step.
- Not every subpackage is equally clean. `saga` is deliberately first-cut and non-durable, while `util` is a pragmatic helper bucket rather than a principled design.
