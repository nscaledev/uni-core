# pkg/server/util

## Intention

`pkg/server/util` is a pragmatic collection of small server-handler helpers that sit at the API boundary and are reused across services.

These helpers are close to request and response handling rather than core domain logic. Today they cover three main jobs:

- writing common response bodies such as JSON and octet-stream payloads
- decoding request-body or query-adjacent data into internal representations
- asserting post-lookup ownership and visibility rules for directly fetched resources

The ownership assertions are the most important non-obvious part of the package. They are not RBAC policy engines. They are concealment helpers for direct object lookup paths where a handler fetches by resource ID first for efficiency, then checks whether the result is actually in the caller's allowed organization or project scope. If the scope does not match, the helper returns a 404 so the API does not leak the existence of an out-of-scope resource.

## Invariants And Guard Rails

- This package is for small server-boundary helpers that are generic across services, not for core business logic.
- `AssertOrganizationOwnership` and `AssertProjectOwnership` are post-lookup scope assertions, not substitutes for RBAC authorization decisions.
- Those ownership assertions exist specifically to preserve "not found" semantics after direct resource lookup when revealing existence would leak information across scopes.
- Response helpers here are intentionally thin wrappers. They do not replace schema validation, business logic, or higher-level error shaping.
- `ReadJSONBody` is intended for paths where earlier OpenAPI schema validation in middleware should already have established the expected body shape. A decode failure at this stage usually indicates a mismatch between that earlier validation contract and later handler expectations.
- Tag decoding helpers translate API-facing OpenAPI parameter forms into internal tag structures. They should stay aligned with the shared OpenAPI contract rather than inventing independent parsing rules.

## Caveats

- The package is useful but not conceptually tight. Response encoding, ownership concealment, and tag decoding are only loosely related beyond all being handler-adjacent concerns.
- `rbac.go` is a misleading filename if read literally. The code does not make authorization decisions; it performs post-lookup ownership assertions with information-hiding behavior.
- The response helpers are intentionally minimal and can silently do very little beyond logging if marshaling or writing fails. They are convenience helpers, not a full response abstraction layer.
- If this package grows much further, it may want splitting by concern rather than continuing as a generic server utility bucket.
