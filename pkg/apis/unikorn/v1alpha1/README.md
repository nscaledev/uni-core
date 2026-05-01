# pkg/apis/unikorn/v1alpha1

## Intention

`pkg/apis/unikorn/v1alpha1` is the shared Kubernetes object-contract substrate for
the platform. It defines the generic types, interfaces, and helper functions that
multiple repositories reuse in their own `pkg/apis/unikorn/v1alpha1` packages so
they do not each reinvent status conditions, semantic versions, IPv4 wrappers, tag
types, or the managed-resource controller contract.

This package also carries one real CRD of its own, `HelmApplication`. That makes
the package slightly compromised: most of it is cross-repository substrate, while
one part is legacy in-tree CD/application schema.

## What Lives Here

- Generic managed-resource interfaces used by the controller/provisioner layer:
  `ResourceLabeller`, `ReconcilePauser`, `StatusConditionReader`,
  `StatusConditionWriter`, and `ManagableResourceInterface`.
- Shared condition vocabulary and helpers:
  `Condition`, `ConditionType`, `ConditionReason`, `GetCondition()`,
  `UpdateCondition()`.
- Shared API value types:
  `SemanticVersion`, `SemanticVersionConstraints`, `IPv4Address`, `IPv4Prefix`,
  `Tag`, `TagList`, `MachineGeneric`, `NetworkGeneric`, and application reference
  types.
- Scheme registration for this repository's core API group.
- `HelmApplication`, which describes installable application templates for the old
  in-tree CD/application model.
- `fake`, a minimal fake managed resource package used by tests and by broad scheme
  assembly in [pkg/client](../../../client/README.md).

## Relationships

- [pkg/provisioners](../../../provisioners/README.md) and
  [pkg/manager](../../../manager/README.md) rely on
  `ManagableResourceInterface` as the generic controller-side resource contract.
- [pkg/server/conversion](../../../server/conversion/README.md) and sibling service
  repositories reuse the shared value types when projecting Kubernetes resources
  into API responses.
- [pkg/provisioners/application](../../../provisioners/application/README.md) uses
  `HelmApplication` as the schema for the legacy in-tree application provisioning
  path.

## Invariants

- If a resource is intended to participate in the generic manager/provisioner
  lifecycle, it must implement the managed-resource interfaces defined here rather
  than inventing a parallel contract elsewhere.
- `Condition`, `ConditionType`, and `ConditionReason` are the repository's common
  status vocabulary for managed resources. Higher layers such as
  [pkg/manager](../../../manager/README.md) assume these meanings when updating or
  interpreting status.
- `SemanticVersion` and `SemanticVersionConstraints` intentionally smooth over the
  platform's version-handling needs, including accepting both `1.2.3` and `v1.2.3`
  forms because surrounding tooling such as Helm is looser than strict semver.
- The IPv4 wrapper types exist so CRDs, JSON serialization, and unstructured
  conversion all agree on one representation instead of every service inventing its
  own string wrappers.

## Caveats

- The package boundary is mixed. Most of the package is generic substrate, but
  `HelmApplication` is a specific legacy CRD tied to the in-tree CD/application
  model. New generic types should be judged carefully so this package does not
  become a dumping ground for unrelated cross-service schema.
- `ManagableResourceInterface` is misspelled and now part of the public contract.
  It is historical baggage, not a naming success.
- The condition vocabulary is intentionally coarse. It supports shared manager and
  conversion logic, but it is not a rich domain-specific state model.
- `fake` is support baggage for tests and scheme assembly. It is useful, but it is
  not a real API surface that production code should start depending on casually.
- `HelmApplication` is a compatibility contract while the old application/CD model
  still exists. If that model is removed, this type and its helpers should become
  deletion candidates.

## Cleanup Targets

- `HelmApplication` can be removed once the remaining in-tree CD/application
  consumers have been replaced and [pkg/provisioners/application](../../../provisioners/application/README.md)
  no longer depends on it.
- The `fake` package should stay narrow. If more elaborate fake CRDs are needed,
  that is usually a sign the test should use the service-specific API package or a
  more local test fixture instead.
