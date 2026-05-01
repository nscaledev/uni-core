# pkg/apis/unikorn

## Intention

`pkg/apis/unikorn` is the versioned Kubernetes API surface owned by this repository.
Today that means [v1alpha1](./v1alpha1/README.md).

This directory is not the definition of every platform CRD. Most service-specific
resources live in sibling repositories under their own `pkg/apis/unikorn/v1alpha1`
packages. What this repository contributes here is the shared substrate those APIs
build on, plus one legacy in-tree application descriptor type.

## Package Map

- [v1alpha1](./v1alpha1/README.md): shared generic API types, managed-resource interfaces, status helpers, semantic-version and networking primitives, and the legacy `HelmApplication` CRD.

## Caveats

- This is a repository API package, not the end-user HTTP API surface. For the HTTP/OpenAPI contract, see [pkg/openapi](../../openapi/README.md).
- The `HelmApplication` side of `v1alpha1` is tied to the in-tree CD/application model. That part should shrink rather than spread as the platform continues moving away from in-tree CD.
