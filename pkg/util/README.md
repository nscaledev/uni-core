# pkg/util

## Intention

`pkg/util` is a legacy miscellaneous bucket. It does not represent one coherent abstraction. It contains a small set of cross-cutting helpers and tiny shared types that historically did not get better homes.

The main things still living here are:

- `GenerateResourceID()` for generating Kubernetes-safe random resource IDs
- `GenerateDeterministicResourceID()` for generating Kubernetes-safe deterministic resource IDs from a UUID v5 hash of a caller-supplied namespace and invariant string
- `ServiceDescriptor` for passing common service/controller identity metadata such as name, version, and revision
- `GetNATPrefix()` for discovering the process's internet-facing address and expressing it as a `/32` so managed cluster firewall rules can allow control-plane access
- `K8SAPITester` and `DefaultK8SAPITester` for a narrow kubeconfig connectivity check seam used by CD-layer code
- `Keys()` as a small map-key helper that is now effectively obsolete

## Invariants And Guard Rails

- This package should stay small. New code should not treat `pkg/util` as the default home for unrelated helper functions just because they feel broadly reusable.
- `GenerateResourceID()` is the shared helper for generating random Kubernetes resource IDs that satisfy the repository's naming expectations.
- `GenerateDeterministicResourceID()` is the shared helper for deriving a stable Kubernetes resource ID from caller-supplied invariant data. The same namespace UUID and invariant string always produce the same name, enabling Kubernetes 409 conflict detection as a deduplication mechanism. Each resource type should use its own fixed namespace UUID constant to prevent cross-type collisions, and the invariant must be composed of stable, immutable fields.
- `ServiceDescriptor` is the shared identity payload used where services, controllers, or cross-service clients need a common `name/version/revision` description.
- `GetNATPrefix()` is a pragmatic helper for the managed-cluster access model. Its job is to discover the control plane's egress address so firewall rules can allow access back into managed clusters.
- `K8SAPITester` is a very limited integration seam for testing whether a kubeconfig can actually reach a Kubernetes API. It is not a broad connectivity abstraction and is mainly relevant to the CD-layer reachability check path.

## Caveats

- The package boundary is weak. These helpers live together mostly because of history, not because they belong to one design.
- `Keys()` is effectively superseded by `maps.Keys` and should be treated as a cleanup candidate rather than something new code should continue to spread. It can be removed once the remaining live call sites are migrated.
- `GetNATPrefix()` depends on an external internet service, memoizes the result globally, and assumes one observed egress address is the right process-wide answer. That is pragmatic operational baggage, not a cleanly modeled networking contract.
- `K8SAPITester` and its default implementation are specific enough that they would make more architectural sense closer to the code that owns kubeconfig validation.
