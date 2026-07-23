# pkg/provisioners

## Intention

`pkg/provisioners` defines the repository's reconciler-oriented provisioning contract. It is the abstraction layer used to drive resource lifecycle through idempotent `Provision(ctx)` and `Deprovision(ctx)` operations that can make partial progress, yield, and converge over repeated controller-runtime retries.

This package itself is intentionally small:

- the `Provisioner` and `ManagerProvisioner` interfaces
- the `RemoteCluster` interface for deriving remote kubeconfigs and identities
- shared metadata for provisioner names
- shared sentinel errors and error dispositions (`ErrYield`, `ErrTerminal`,
  `ErrUserActionRequired`) plus the `Error` carrier type

Most of the real behavior lives in the lower-level adapters and combinators under this directory.

## Error Dispositions

A provisioner communicates *what the controller should do next* through the
**disposition** of the error it returns. The disposition — not a human-readable
message — is the contract; the manager branches on it with `errors.Is`. This is
why dispositions are sentinels, never prose.

| Disposition | Returned by | Manager behaviour | Recovery |
| --- | --- | --- | --- |
| `nil` | success | mark `Available: Provisioned` | n/a |
| `ErrYield` | `ErrYield`, `Yield(reason, message)`, `DependencyNotReady`/`DependencyFailed` | write `Available: False`; requeue on the fixed yield timeout | next reconcile |
| `ErrTerminal` | `Terminal(reason, message)`, `DependencyNotFound` | write `Available: False`; **stop requeuing** | operator intervention |
| `ErrUserActionRequired` | `UserActionRequired(reason, message)` | write `Available: False`; **stop requeuing** | a spec change (generation bump) wakes the controller |

`ErrTerminal` and `ErrUserActionRequired` are *terminal*: requeuing them just
burns the workqueue on a failure that will not self-heal. Use
`provisioners.IsTerminal(err)` to test for either. They differ only in how a
resource is revived — that difference lives in the watch/predicate layer, not in
the requeue decision.

### The `Error` carrier: `reason` + `message`

`Error` pairs a disposition with two surfaced fields, both user-safe, that map
one-to-one onto the `Available` condition:

- `reason` — a closed-vocabulary `unikornv1.ProvisioningConditionReason` (e.g.
  `DependencyNotReady`). Machine-classifiable and written straight onto the
  condition's `Reason` field. Typed rather than a bare string so the vocabulary
  is explicit at every call site; the API projection classifies each reason by
  its coarse disposition (see the server `conversion` package).
- `message` — user-safe human detail (e.g. `Network "prod-net" (a1b2) is not
  ready`), written onto the condition's `Message` field. It **is** shown to the
  user, so it must be safe: name the user's own resources, never internal
  topology.

The manager surfaces these directly via `SetProvisioningCondition` — reason to
`Reason`, message to `Message` — through the typed accessors `Reason()` and
`Message()`. There is no flattening into a single string: with `metav1.Condition`
the reason is a first-class field, so tooling reads `Reason` natively and the
human reads `Message`.

Genuinely operator-only detail (provider internals, raw upstream errors) must
**not** go in `message` — wrap the `Error` with `fmt.Errorf` instead. `Error()`
embeds the wrapping for logs, and the manager's `errors.As` recovers only the
typed `Reason()`/`Message()`, so the wrapping never reaches the user surface
(CWE-209). New failure modes should return a typed `Error`: a bare error falls
back to the stringified error on the condition, a known fail-open leak.

`Yield(reason, message)` is the `ErrYield` sibling of `Terminal` and
`UserActionRequired`. Dependency waits are raised only through the enforced
constructors — `DependencyNotReady` / `DependencyFailed` (both yield) and
`DependencyNotFound` (terminal) — which take the dependency object, bind the
reason and disposition together, and compose a safe message via the unexported
`describeResource` (Kind from the scheme's GVK, the mutable display name, and the
durable id). Binding them in one call is deliberate: a caller cannot pair a
`DependencyNotFound` with a yield that would spin forever, or forget the
description.

### Caveat: terminality is not yet wired everywhere

The dispositions exist and the manager honours them, but the providers that
should *emit* them mostly still return bare errors or `ErrYield`. Adopting them
is incremental. In particular, a controller that uses `ErrUserActionRequired`
must also reset any per-resource retry bookkeeping on a generation change, or the
resource will immediately re-enter the terminal state instead of retrying.

## Invariants And Guard Rails

- Provisioners are intended to run inside reconcilers. They are expected to be idempotent and safe to revisit across repeated reconcile passes.
- `ErrYield` is a core part of the model, not an edge case. It means "stop now and let the controller retry later" when a provisioner would otherwise block for too long waiting on external progress.
- The preferred progress model is framework-driven retry, not long local retry loops. Provisioners should generally fail or yield quickly and let controller-runtime handle fairness and requeue.
- `Deprovision(ctx)` is part of the same convergence model. It may make partial progress and return `ErrYield` while waiting for external deletion or teardown to complete.
- `ManagerProvisioner` is the top-level provisioner shape that bridges directly into the controller-runtime layer for managed resources.
- `RemoteCluster` is the narrow interface used by remote-scope provisioners to derive the target cluster identity and kubeconfig.

## Package Map

- [application](./application/README.md): CD-managed Helm application provisioner. Resolves application/version, derives application identity from resource scope, applies generator customizations, and delegates lifecycle to the CD driver.
- [remotecluster](./remotecluster/README.md): switches active provisioning scope into a remote cluster, constructs the remote client, and coordinates shared remote lifecycle for descendants.
- [serial](./serial/README.md): ordered composition combinator for dependency-sensitive children. Provision in order, deprovision in reverse order.
- [concurrent](./concurrent/README.md): parallel composition combinator for independent children that should make progress in the same reconcile pass.
- [conditional](./conditional/README.md): binary desired-state gate where predicate false means actively deprovision the child, not merely skip it.
- [resource](./resource/README.md): legacy single-`client.Object` adapter. Historical hack, not a recommended pattern for new code.
- [util](./util/README.md): small provisioner-side helper bucket, mostly scheduling/config-generation fragments and a couple of operational helpers.

## Caveats

- `pkg/provisioners` is a contract layer, not a clean architecture in itself. Much of the real complexity is pushed into the subpackages, especially `application` and `remotecluster`.
- The whole model assumes reconciler-friendly behavior: idempotence, quick yields, and retry-based convergence. If child provisioners block internally or depend on one-shot success, the abstraction breaks down.
- Several important subpackages are tightly coupled to the older context-scoping and in-tree CD model documented in [pkg/client](../client/README.md), especially [application](./application/README.md) and [remotecluster](./remotecluster/README.md).
- Not every subpackage here is equally healthy. `resource` is legacy baggage, and some of the extension seams in `application` are historical compromise rather than clean design.
