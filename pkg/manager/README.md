# pkg/manager

## Intention

`pkg/manager` is the controller-runtime integration layer for the platform's reconciler/provisioner model. It is the package that turns the lower-level provisioner contract into an operational controller process with shared startup, reconcile, teardown, status, and deletion-ordering behavior.

This package is where several lower-level ideas become enforceable runtime policy:

- the reconciler-oriented provisioner contract from [pkg/provisioners](../provisioners/README.md)
- the "yield instead of block" progress model driven by `provisioners.ErrYield`
- the context-scoping and client-access model described in [pkg/client](../client/README.md)
- the shared runtime/bootstrap options from [pkg/options](../options/README.md)
- the controller-specific option layer in [options](./options/README.md)

In practice `pkg/manager` does three jobs:

- bootstraps controller processes through `Run(f ControllerFactory)`
- implements the generic reconcile loop through `Reconciler`
- manages cross-resource deletion ordering through resource-reference helper functions

## Invariants And Guard Rails

- `ControllerFactory` is the top-level integration contract for controller binaries built on this package.
- `Run(f ControllerFactory)` is framework bootstrap code, not a generic library helper. It owns flag wiring, logging, OpenTelemetry setup, optional upgrade/initialize hooks, manager creation, controller creation, watch registration, and process startup.
- `Reconciler` is the shared reconcile-policy implementation for manager-style resources. It injects the shared execution context that descendant provisioners depend on before invoking them.
- That injected context includes:
  - manager access via `pkg/manager`
  - namespace and static Kubernetes client via [pkg/client](../client/README.md)
  - active cluster scope via `client.ClusterContext`
  - CD driver context
  - managed-resource context for [pkg/provisioners/application](../provisioners/application/README.md)
- `provisioners.ErrYield` is a first-class control signal here. It means "stop now and requeue on the fixed yield timeout" rather than "return a hard reconcile error."
- During normal reconcile, even unexpected provisioner errors are intentionally translated into status updates plus fixed requeue, rather than returned as raw reconcile errors, to avoid controller-runtime exponential backoff harming throughput.
- Terminal dispositions (`provisioners.ErrTerminal`, `provisioners.ErrUserActionRequired`, tested via `provisioners.IsTerminal`) are the exception to the requeue-everything rule **on the provision path only**: the condition is written, but the resource is **parked** — no requeue — because retrying a non-self-healing failure only burns the workqueue. Revival is out-of-band: a spec change (generation bump) wakes an `ErrUserActionRequired` resource through the consumer's watch predicate, while `ErrTerminal` awaits operator intervention. This currently piggybacks on the `Errored` condition reason rather than introducing a new enum value; the distinction is in the requeue decision, not the surfaced condition.
- The delete path does **not** honour terminal dispositions: a `Deprovision` that returns a terminal error is treated as an ordinary hard error (returned to controller-runtime, exponential backoff), because parking a deletion would strand the finalizer and leak the resource. Do not return `Terminal()`/`UserActionRequired()` from `Deprovision` expecting it to park — yield and keep converging instead.
- The user-visible condition message is sanitised through `provisioningFailureMessage`: a typed `provisioners.Error` contributes only its closed-vocabulary `Reason()`, never its operator-only `Why()`. Bare (untyped) errors fall back to the legacy stringified form, which is a known fail-open leak and the reason new failure modes should return a typed error.
- During delete reconcile, synthetic resource references and owned-resource finalizers are checked before child deprovisioning is allowed to proceed.
- The resource-reference helpers implement the platform's deletion-ordering contract by encoding references as extra finalizers on referenced resources.
- `ResourceReady()` is the shared readiness gate for dependent resources and returns `provisioners.ErrYield` when a dependency is not yet provisioned.

## Lower Layers

- [options](./options/README.md): controller-specific options built on [pkg/options](../options/README.md), including max concurrency and CD driver selection.
- [provisioners](../provisioners/README.md): the child lifecycle contract that this package drives.
- [client](../client/README.md): namespace/client/cluster context propagation used by the reconciler.
- [provisioners/application](../provisioners/application/README.md): receives the managed-resource and CD contexts injected here.

## Caveats

- The package boundary is broad. It mixes process bootstrap, reconcile policy, context propagation, readiness helpers, and deletion/reference semantics.
- `pkg/manager` is tightly coupled to the in-tree CD model. `getDriver()` currently hardcodes ArgoCD driver selection and treats anything else as unsupported.
- The shared reconcile loop relies on substantial hidden context setup before provisioners run. That is efficient for repository consistency, but it means many downstream components depend on implicit prerequisites rather than explicit method arguments.
- The deletion-ordering model is powerful but also compromised: resource references are encoded as finalizers, and `GenerateResourceReference()` still carries legacy naming baggage including the `unikorn-cloud.org` to `kubernetes.unikorn-cloud.org` group rewrite.
- `Run()` exits the process directly on setup failures. That is appropriate for controller binaries, but it reinforces that this package is an operational framework layer rather than a clean reusable library.
