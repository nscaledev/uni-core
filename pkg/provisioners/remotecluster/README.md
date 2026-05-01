# pkg/provisioners/remotecluster

## Intention

`pkg/provisioners/remotecluster` is the provisioner adapter for switching execution into a remote cluster scope. It is not just a convenience wrapper for "run this on another cluster." It coordinates remote-cluster lifecycle, remote client construction, and child provisioner execution inside that remote scope.

This package is one of the concrete places where the context-scoping model described in [pkg/client](../../client/README.md) becomes operational. Context here is not incidental metadata. It carries the active provisioning scope, and this package rewrites that scope so descendant provisioners operate against a remote cluster instead of the local control-plane view.

In practice the package does three things:

- ensures the remote cluster itself is created or registered when this wrapper owns that responsibility
- builds a Kubernetes client for the remote kubeconfig and installs a new cluster scope into context
- runs a child provisioner inside that remote scope, and later coordinates its teardown before the remote itself is removed

## Invariants And Guard Rails

- `ProvisionOn(child, ...)` returns a provisioner that executes the child inside a remote cluster scope, not in the caller's current cluster scope.
- The remote scope is installed by creating a new `client.ClusterContext` and attaching it to context. Descendant provisioners are expected to treat that as the active provisioning target, consistent with the `pkg/client` context model.
- When the `RemoteCluster` wrapper is marked as the controller, it owns remote cluster registration/lifecycle and ensures the remote exists before child provisioning proceeds.
- Multiple child provisioners can share one `RemoteCluster` wrapper. The internal reference counting exists so that shared remote lifecycle is created once and torn down only after all registered children have completed.
- `backgroundDeletion` is propagated through context to descendants. It is a policy signal that the remote is being discarded and some child cleanup may be safely skipped when the remote itself will be destroyed anyway.
- The package is intended for reconciler-driven use. Child provisioners are expected to be idempotent and to yield quickly when they cannot make progress.

## Caveats

- This package mixes several concerns in one place: remote lifecycle ownership, remote client construction, provisioning-scope context switching, descendant policy propagation, and shared reference counting.
- Correctness depends on usage discipline. Callers must consistently reuse the same `RemoteCluster` wrapper when multiple child provisioners are meant to share one remote lifecycle; otherwise the internal reference counting model does not describe reality.
- The package is tightly coupled to the context-scoping model from [pkg/client](../../client/README.md). If that model is retired along with older CD/Argo-style provisioning flows, this package would likely need to shrink or be redesigned as well.
- `backgroundDeletion` is a hidden context contract rather than an explicit method parameter on descendants, which makes behavior convenient but also less obvious at call sites.
- During deprovision, failure to build the remote client due to `provisioners.ErrYield` is treated as a signal that child deprovisioning can be skipped because the remote is already effectively gone. That is a pragmatic lifecycle shortcut, not a universally safe remote-cleanup rule.
