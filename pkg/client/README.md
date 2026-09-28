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
- The client certificate MUST be reloaded on every TLS handshake. Caching it on a timer strands a service on a certificate the ingress no longer trusts once the client CA moves, and no amount of retrying fixes that without a restart.
- The reload MUST NOT be held under a lock. Parsing is the expensive part and a lock around it serializes every concurrent handshake in the process.
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

## Client certificate reload

`GetClientCertificate` loads and parses the certificate from the Secret on every
handshake, so a rotated CA or leaf takes effect immediately with no restart.

This replaced a 24-hour reload interval, which meant a service whose pod started
less than a day before a CA rotation kept presenting the old certificate and was
refused by the ingress until it was restarted. That cost 23 restarts during a
production rotation.

The cost of reloading every time is a parse, measured on Go 1.26:

| key pair | `tls.X509KeyPair` |
| --- | --- |
| RSA 4096 | 0.7-1.1 ms |
| RSA 2048 | 154 us |
| ECDSA P-256 | 26 us |
| certificate alone, RSA 4096 | 3.5 us |

Almost all of it is private-key parsing and validation, not the certificate, so
the key algorithm matters far more than caching does.

The read on top of that is only cheap if the caller supplies a cache-backed
client, as `New()` does. Nothing here enforces that: `ApplyTLSClientConfig`
accepts any `client.Client`, and with an uncached one every handshake is an API
round trip, uncoalesced. Pass a cached client.

Handshakes are infrequent in steady state because connections are pooled, so the
API servers, which construct their clients once at startup, pay this only on a
reconnect.

Callers that construct a client per call pay it twice: once for the
construction-time load and once for the handshake, where a timer-based cache
paid it once. That is the controllers, per reconcile, via a new `http.Transport`
each time. Reconciliation is asynchronous and not user facing, so the extra
parse is accepted; the construction-time load is kept because it is what makes a
missing or malformed Secret fail at startup instead of at first request.

A reload that fails serves the last loaded certificate. That is deliberate, but
it is logged, at most once a minute, because a Secret that cannot be read means
the service is running on a certificate the ingress will reject at the next
rotation. Rate limiting rather than edge triggering: an edge flag turns an
intermittent failure into one line per handshake, and two concurrent handshakes
can apply it out of order and leave it stuck, swallowing the next real failure.

Each load runs on the caller's context, capped by a timeout. At construction
that is the setup context; on a handshake it is the handshake's own, so a caller
that has already given up is not held here.

## Caveats

- The package name is misleading. `pkg/client` conflates unrelated responsibilities for legacy reasons and should be split into more intuitive packages.
- A sensible split would be: Kubernetes client construction, internal HTTP client security support, and context-based provisioning scope.
- The context-based scoping model is tied to the old ArgoCD/in-tree CD layer. It is legacy machinery that should shrink with that architecture rather than spread further.
- The HTTP MTLS helper layer may become obsolete if service identity and transport concerns move out to something like SPIFFE.
- `New()` starts the controller-runtime cache asynchronously and discards cache startup failure. Constructor success does not prove that the cache has started successfully.
- `NewScheme()` is convenience-biased rather than minimal. It includes historical and compatibility-oriented registrations so callers in services or tests tend to get something that works for this repository.
- The Argo shim and other CD-layer baggage remain in lock step with this repository for compatibility, but that is legacy behavior rather than a direction to build more dependencies around.
- CA trust loading and client-certificate loading do not use identical configuration rules: CA loading only checks whether the secret name is set, while client-certificate loading requires both namespace and name. This is a current inconsistency and a potential bug if this package continues to own MTLS setup.
- Post-startup certificate reload is best-effort. If a reload fails after an earlier certificate was loaded successfully, the process keeps using the stale certificate rather than failing closed.
- Reload uses a background context so it does not inherit the setup path's cancellation, but each load is bounded by a timeout, and a load that fails falls back to the last good certificate.
- The payload signing helpers are currently RSA-only. That restriction applies to message signing and verification, not to MTLS in general.
