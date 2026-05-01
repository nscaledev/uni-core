# pkg/apis

## Intention

`pkg/apis` contains Kubernetes API packages owned or carried by this repository.
It is not the platform's entire CRD universe. Most service-specific APIs live in
sibling repositories under their own `pkg/apis/unikorn/v1alpha1` trees.

In this repository there are two different stories:

- [unikorn](./unikorn/README.md): the real shared API substrate used across
  repositories, plus the legacy in-tree `HelmApplication` CRD.
- `argoproj`: local compatibility types for legacy ArgoCD/CD integration. These
  are baggage from the old in-tree CD model, not a healthy foundation to build
  more new API surface on.

## Package Map

- [unikorn](./unikorn/README.md): shared generic types, managed-resource
  interfaces, status vocabulary, and the legacy `HelmApplication` schema.

## Caveats

- This tree is about Kubernetes object schema, not HTTP/OpenAPI schema. For the
  service API contract, see [pkg/openapi](../openapi/README.md).
- Not every API package here deserves equal strategic weight. `unikorn` is core
  shared substrate. The Argo-related side is legacy compatibility surface that
  should shrink rather than spread.
