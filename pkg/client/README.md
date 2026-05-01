# pkg/client

## Intention

`pkg/client` is a historical catch-all package for client-related concerns inside platform processes. It exists so repository-wide client behavior can be fixed once in one place, not because these responsibilities form a clean abstraction.

Today it conflates three main functions:

- Kubernetes client and scheme construction for processes that genuinely need to create their own in-cluster Kubernetes client
- HTTP client security support for internal service-to-service API traffic, including MTLS material loading and principal-propagation signing around generated API clients
- context-based client scoping for hierarchical provisioner execution as control moves from the local ArgoCD-managed layer into remote clusters

The Kubernetes side is the sanctioned constructor path when a process genuinely needs to create its own in-cluster client. If an appropriate client is already available, use that instead of constructing another one. The point is to avoid ad hoc client construction scattered across the codebase.

The HTTP side exists because generated API clients provide bindings, but not the platform's internal trust model. This package loads CA and client-certificate material from Kubernetes secrets, applies it to TLS configuration, and signs principal-bearing payloads so internal services can prove possession of the X.509 private key associated with their identity.

The context side is legacy provisioner plumbing. It carries the active provisioning scope through a call graph so descendant provisioners operate on the currently scoped cluster by default, while still allowing explicit access to the local control-plane client when a step needs to reach back to the ArgoCD-managed layer. This was practical when deeply refactoring function-signature chains was expensive, but it is not a clean boundary.

## Invariants And Guard Rails

- This is an internal platform support package, not a general-purpose client library.
- `New()` is the centralized in-cluster Kubernetes client constructor for processes that genuinely need to build their own client. Do not create Kubernetes clients ad hoc in random code paths.
- If you are running inside a controller-runtime manager or controller, use the client provided there. For other processes such as APIs, monitors, or similar standalone components, use this package rather than open-coding client construction.
- `NewScheme()` returns the repository's broad convenience scheme, not a minimal scheme. It intentionally registers Kubernetes types, Unikorn API types, fake Unikorn API types, and the local Argo shim before applying any extra `SchemeAdder` functions.
- The Argo/CD-related scheme content exists for legacy compatibility with the old in-tree CD layer. New usage should not grow around it.
- The HTTP client role here is transport and authentication support around internal generated clients, not API generation.
- While this package still owns MTLS setup, it assumes certificate issuance and rotation are handled by an external system rather than by the client code itself.
- TLS trust bundles and client certificates must come from correctly shaped `kubernetes.io/tls` secrets when using the current secret-backed MTLS path.
- Payload signing is part of the current internal trust model for principal propagation between services. It is not a generic invitation to invent new signed application protocols.
- Context scoping in this package controls the active provisioning target. Descendant provisioners are expected to operate on the currently scoped cluster unless they explicitly reach back to the local provisioner client.
- Context-based scoping is legacy CD-layer plumbing and should be treated as constrained internal machinery, not as a pattern to spread further.

## Caveats

- The package name is misleading. `pkg/client` conflates unrelated responsibilities for legacy reasons and should be split into more intuitive packages.
- A sensible split would be: Kubernetes client construction, internal HTTP client security support, and context-based provisioning scope.
- The context-based scoping model is tied to the old ArgoCD/in-tree CD layer. It is legacy machinery that should shrink with that architecture rather than spread further.
- The HTTP MTLS helper layer may become obsolete if service identity and transport concerns move out to something like SPIFFE.
- `New()` starts the controller-runtime cache asynchronously and discards cache startup failure. Constructor success does not prove that the cache has started successfully.
- `NewScheme()` is convenience-biased rather than minimal. It includes historical and compatibility-oriented registrations so callers in services or tests tend to get something that works for this repository.
- The Argo shim and other CD-layer baggage remain in lock step with this repository for compatibility, but that is legacy behavior rather than a direction to build more dependencies around.
- CA trust loading and client-certificate loading do not use identical configuration rules: CA loading only checks whether the secret name is set, while client-certificate loading requires both namespace and name. This is a current inconsistency and a potential bug if this package continues to own MTLS setup.
- Client certificate reload is lazy and handshake-triggered rather than proactive.
- Post-startup certificate reload is best-effort. If a reload fails after an earlier certificate was loaded successfully, the process keeps using the stale certificate rather than failing closed.
- Reload uses a background context, so post-construction certificate refresh is not currently time-bounded.
- The payload signing helpers are currently RSA-only. That restriction applies to message signing and verification, not to MTLS in general.
