# pkg/provisioners/serial

## Intention

`pkg/provisioners/serial` is the ordered-composition combinator for provisioners whose lifecycle has ordering constraints. It is intended for reconciler-driven use, where child provisioners are expected to be idempotent and repeated reconcile passes are expected to drive the system toward convergence.

This package does not try to provide transactional rollback. Its job is simpler:

- provision children in dependency order
- deprovision children in reverse dependency order
- stop on the first error or yield and let the reconciler come back later

## Invariants And Guard Rails

- `Provision(ctx)` executes child provisioners in the declared order.
- `Deprovision(ctx)` executes child provisioners in reverse order.
- Execution stops on the first returned error, including `provisioners.ErrYield`.
- This combinator assumes child provisioners are idempotent and safe to revisit on later reconcile passes.
- The intended convergence model is controller-driven retry, not one-shot success.
- Reverse-order deprovision assumes the same ordered child list is valid for both creation and teardown, with teardown simply needing the inverse traversal.

## Caveats

- This package preserves ordering only. It does not attempt rollback or compensation for earlier successful provision steps when a later child fails.
- Partial progress is expected and acceptable in the reconciler model, but only because callers are expected to supply idempotent children and rely on automatic retries.
- If the declared ordering is wrong, this combinator will faithfully enforce that wrong ordering.
- Because execution stops on first error or yield, later children remain untouched until a future reconcile pass resumes progress.
