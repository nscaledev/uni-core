# pkg/provisioners/resource

## Intention

`pkg/provisioners/resource` is a legacy adapter that wraps a single Kubernetes `client.Object` in the provisioner interface. It is not a recommended abstraction for new provisioner code.

This package exists mostly as historical convenience for trivial single-object cases, likely things such as namespaces, where someone wanted to fit a plain Kubernetes object into the provisioner lifecycle without writing a purpose-built provisioner.

## Invariants And Guard Rails

- New provisioner code should avoid this package. It is legacy compatibility, not the preferred model for resource management.
- `Provision()` simply applies `controllerutil.CreateOrUpdate()` to the provided object using the scoped client from provisioner context.
- `Deprovision()` simply issues a delete for the provided object, treats not-found as success, and otherwise returns `provisioners.ErrYield` so the broader reconcile loop can come back while deletion completes.
- The object must already be fully prepared by the caller. This package does not generate, enrich, or orchestrate resource state beyond the raw create-or-update/delete operations.

## Caveats

- The type and constructor comments are stale and misleading. The implementation does not parse YAML manifests and does not invoke `kubectl`.
- This package adds almost no real abstraction beyond adapting a single `client.Object` to the provisioner interface.
- It is only suitable for extremely trivial single-object cases. Anything with meaningful generation logic, multiple resources, lifecycle coordination, or nontrivial semantics should have a purpose-built provisioner instead.
- This is a deletion candidate once the remaining namespace lifecycle call sites, currently in the identity organization and project provisioners, are rewritten as explicit namespace create/delete handling rather than funneled through this adapter.
