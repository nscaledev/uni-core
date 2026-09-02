# pkg/manager/options

## Intention

`pkg/manager/options` is the controller-specific layer on top of
[pkg/options](../../options/README.md). It defines the common runtime flags needed
by controller binaries in this repository after the generic process bootstrap
concerns have already been handled.

This package is not a general controller configuration framework. It is the shared
flag surface for controller processes that use [pkg/manager](../README.md).

## What Lives Here

- `Options`, which embeds `options.CoreOptions` and adds:
  - `MaxConcurrentReconciles`
  - `RequeuePeriod`
  - `CDDriver`
- `AddFlags()`, which registers those controller-specific flags and seeds the
  default CD driver.

## Relationships

- [pkg/options](../../options/README.md) provides the common process-bootstrap
  layer for logging, telemetry, namespace scoping, and similar runtime concerns.
- [pkg/manager](../README.md) is the runtime layer that consumes these options when
  starting controller processes.

## Invariants

- Controller binaries should embed or reuse this options struct rather than
  re-declaring common manager flags in each command.
- `MaxConcurrentReconciles` is the shared tuning knob for controller throughput and
  memory tradeoffs.
- `RequeuePeriod` tunes only *how often* a polling controller revisits a settled
  resource, never *whether* it polls. Polling is opted into in code, by the factory
  passing `manager.WithPolling()` to `manager.NewReconciler`, because whether a
  resource needs re-observation is a property of what backs it rather than an
  operator preference. The flag therefore has a non-zero default and is inert for
  every controller that has not opted in. A non-positive value is not a supported
  way to turn polling off: a polling reconciler falls back to
  `constants.DefaultRequeuePeriod` and logs it, because a polling controller that
  never polls goes stale while still reporting success. Note the period also
  interacts with
  `MaxConcurrentReconciles`: a polling controller offers roughly one reconcile per
  resource per `RequeuePeriod` to a worker pool of `MaxConcurrentReconciles`, so a
  short period on a large fleet queues re-observations ahead of new work.
- `CDDriver` exists because the manager layer still carries legacy in-tree CD
  integration and needs one common way to select that backend.

## Caveats

- This package inherits the same strategic blemish as [pkg/manager](../README.md):
  the CD-driver flag is legacy surface tied to the old in-tree CD model.
- The option set is intentionally small. If more flags accumulate here, that is a
  sign the controller layer may be leaking service-specific policy into what should
  stay a shared manager bootstrap surface.
