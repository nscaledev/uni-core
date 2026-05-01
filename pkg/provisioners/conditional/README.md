# pkg/provisioners/conditional

## Intention

`pkg/provisioners/conditional` is a small provisioner combinator for optional subordinate resources. It gates a child provisioner behind a predicate, but the key behavior is stronger than simple skipping: when the predicate is false, the child is actively deprovisioned.

This makes the package useful for feature- or capability-dependent child provisioners where the desired state is binary:

- predicate true: the child should exist
- predicate false: the child should not exist

## Invariants And Guard Rails

- `Provision(ctx)` does not mean "try to provision if enabled and otherwise do nothing". It means "drive the child to the desired existence state for the current predicate value."
- If the predicate is true, `Provision(ctx)` delegates to the child provisioner's `Provision(ctx)`.
- If the predicate is false, `Provision(ctx)` delegates to the child provisioner's `Deprovision(ctx)`.
- `Deprovision(ctx)` always delegates to the child provisioner's `Deprovision(ctx)`, regardless of predicate value, so cleanup still happens during parent teardown even when the child was conditionally disabled.
- This combinator is appropriate when predicate false really means the child resource must not exist.

## Caveats

- The predicate is just `func() bool`. It carries no context, error handling, or structured explanation of why the child is enabled or disabled.
- This package encodes removal semantics, not skip semantics. If a caller wants "disabled for now, but preserve existing child state," this is the wrong abstraction.
- Because false maps to deprovision, callers must be deliberate about using predicates that can flap.
- The package is intentionally tiny and does not add lifecycle policy beyond delegating to the child provisioner based on the predicate.
