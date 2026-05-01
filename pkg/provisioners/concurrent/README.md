# pkg/provisioners/concurrent

## Intention

`pkg/provisioners/concurrent` is the parallel-composition combinator for provisioners that do not depend on each other's ordering and can make progress in the same reconcile pass.

It is intended for reconciler-driven use, where child provisioners are expected to be idempotent, quick to yield, and safe to revisit on later retry passes.

## Invariants And Guard Rails

- `Provision(ctx)` starts all child provisioners concurrently and waits for them all to return.
- `Deprovision(ctx)` starts all child deprovision operations concurrently and waits for them all to return.
- This combinator does not fail fast at the group level. A failure or yield from one child does not stop siblings that are already running in the same pass.
- The intended model is that child provisioners themselves fail or yield quickly rather than blocking for long periods.
- This package is appropriate only when children are genuinely independent and the system benefits from making parallel progress in one reconcile pass.
- The broader convergence model is still controller-driven retry. Partial progress is expected and later passes are expected to complete the remaining work.

## Caveats

- This package does not manage dependencies. If children actually require ordering, the wrong combinator has been chosen.
- The returned error is only the first one preserved by `errgroup`, even if multiple children failed. The implementation logs child failures individually to reduce observability loss, but the API surface still collapses them.
- Because the group does not try to cancel sibling work on the first failure, this combinator depends on child provisioners being reconciler-friendly and fast to yield instead of sitting in long local retry loops.
- This is not a supervisor for long-running blocking tasks. If children block excessively, concurrent composition amplifies that bad behavior rather than fixing it.
