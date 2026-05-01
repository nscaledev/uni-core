# pkg/constants

## Intention

`pkg/constants` is the repository's shared control vocabulary. It exists so platform code uses one canonical set of names and values for resource metadata and a small set of common orchestration semantics.

Most of the package defines metadata contract shared across services, controllers, provisioners, and API conversion layers. That includes labels and annotations for naming, description, creator and modifier attribution, timestamps, organization and project scoping, user linkage, cluster attachment, application lookup, allocation linkage, and generic referenced-resource linkage.

It also defines principal-prefixed metadata keys used when an intermediate service acts on a user's behalf and the platform still needs access to the original principal's identity context for attribution, quota, billing, or scoping decisions.

The remainder of the package defines a few shared operational constants: the common finalizer token used by this management layer for explicit deletion control, the default yield timeout used for controlled retries, and the label-priority ordering used for specific label-tuple-based identity construction paths that need deterministic ordering from a map.

## Invariants And Guard Rails

- This package is a shared contract package, not a miscellaneous bucket for arbitrary constants.
- Code that reads or writes the platform's standard labels and annotations should use these constants rather than open-coded strings.
- The metadata keys here are part of the platform's resource contract. Changing them is a cross-repository compatibility concern, not a local refactor.
- Principal-prefixed metadata is part of the platform's attribution and scoping model when services act on behalf of users. It is not decorative metadata.
- `LabelPriorities()` defines the repository's canonical ordering for the label-tuple identity paths that depend on it. Callers should not invent local ordering rules for the same purpose.
- `Finalizer` is the shared deletion-control token for this repository's management layer where cleanup requires explicit logic rather than raw Kubernetes garbage collection.
- `DefaultYieldTimeout` is the shared default for controlled retry and yield behavior where reconciliation or provisioning work should back off and give another actor a turn.

## Caveats

- The package is mostly coherent as shared metadata and control vocabulary, but it is broader than pure metadata schema because it also carries a few operational defaults.
- Several constants are tied to legacy in-tree CD or Argo-related workflows, especially application-oriented labels. They remain part of the current contract while those flows still exist, but they are candidates for deletion once the remaining ArgoCD and in-tree CD consumers have either been removed entirely or switched to whatever replaces those workflows.
- Some names reflect historical model choices rather than ideal terminology. `PhysicalNetworkAnnotation` is a concrete example: its deprecation is intended, but removal requires downstream services such as `kubernetes` to migrate to the newer network identifier used by the current region and compute flows.
- Centralization here improves consistency today, but some constants are only here because this repository became the aggregation point. Over time, some domain-specific metadata may be better owned by the package or repository that defines the primitive, for example organization and project metadata in identity-owned code and network metadata in region-owned code.
- `DeveloperVersion` is legacy sentinel baggage from older developer-only behavior rather than a strong part of the package model.
