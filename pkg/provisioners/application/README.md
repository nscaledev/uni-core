# pkg/provisioners/application

## Intention

`pkg/provisioners/application` is the provisioner adapter for CD-managed Helm applications. It turns a managed resource plus an application lookup function into the canonical CD driver operations needed to create, update, and delete an application instance.

This package is not a generic Helm abstraction and it is not merely a thin wrapper around the CD driver. It encodes the repository's model for:

- resolving which version of a `HelmApplication` applies to the current managed resource
- deriving a stable application identity from that resource
- translating resource scope, remote-cluster scope, and optional generator customizations into a `cd.HelmApplication`
- delegating the actual application lifecycle to the CD driver

Like [pkg/provisioners/remotecluster](../remotecluster/README.md), this package is tightly coupled to the context-based provisioning-scope model described in [pkg/client](../../client/README.md). It expects context to carry both the managed resource and the active cluster scope that determine where the application should be installed.

## Invariants And Guard Rails

- Within the current in-tree CD/application model, this is the main provisioner adapter for CD-managed Helm applications.
- `New(applicationGetter)` requires a lookup function that resolves the effective `HelmApplication` object and version for the current context. The provisioner does not own application discovery itself.
- `Provision()` and `Deprovision()` both call `initialize()` at execution time, not at construction time, so application resolution happens in a path that can return normal reconcile errors.
- Application identity is derived from both sides of the relationship: the application name selected during initialization and the managed resource labels carried in context. Those resource labels are sorted deterministically so the CD-layer identity remains stable.
- Destination cluster identity comes from the active `client.ClusterContext` in context. Descendants are therefore expected to run under the correct provisioning scope before this provisioner is invoked.
- `InNamespace()` overrides the application namespace explicitly. Otherwise the namespace comes from the application version, falling back to `default`.
- `WithGenerator()` is the historical customization seam for adding implicit release names, parameters, values, namespace metadata, ignored-difference customizations, and lifecycle hooks around an otherwise standard application template.
- `AllowDegraded()` deliberately weakens the success condition so degraded application health is accepted for cases where that is an intentional repository policy.
- `PreDeprovisionHook` runs before application deletion and `PostProvisionHook` runs only after successful provisioning.
- Deprovision propagates `remotecluster.BackgroundDeletionFromContext(ctx)` into the CD driver's delete path so descendant cleanup can respect doomed-remote semantics.

## Caveats

- This package mixes several concerns in one place: application lookup, version selection, identity derivation, template customization, lifecycle hooks, remote-scope interpretation, and CD-driver delegation.
- The package is heavily context-driven. It assumes the managed resource is already present in context and that the active cluster scope has already been set correctly. If either hidden prerequisite is missing or wrong, behavior will be wrong in ways the constructor cannot prevent.
- `WithGenerator()` is intentionally open-ended and therefore compromise-prone. It is a plugin-style `any` plus a pile of optional interface assertions, not a clean extension model. The typed generator interfaces are useful, but the generic `Customizer` hook is effectively an escape hatch and should be treated with suspicion.
- `AllowDegraded()` is not a neutral option. It encodes a policy exception that should only be used when degraded application health is genuinely acceptable for that application.
- `PreDeprovisionHook` and `PostProvisionHook` are operational escape hatches around the main CD lifecycle. Useful, but also evidence that some managed applications still need extra bespoke handling.
- `getResourceID()` still depends on `util.Keys()` for deterministic label ordering. That is one of the remaining obstacles to deleting the now-obsolete helper in `pkg/util`.
- This package is coupled to the CD/application model. If the old in-tree CD layer continues to shrink or is replaced, this package would likely need to be split or redesigned rather than carried forward unchanged.
