# pkg/server/saga

## Intention

`pkg/server/saga` provides a deliberately small in-process saga runner for multi-step server workflows that need best-effort cleanup on failure.

Its purpose is to give handlers a common rollback pattern for synchronous operations that perform several state-changing steps, such as quota allocation followed by resource creation. Instead of open-coding partial rollback in every handler, callers describe an ordered list of forward actions and optional compensation actions.

This package is intentionally a first-cut implementation. The goal is to get server workflows onto a shared best-effort cleanup model, not to provide durable orchestration, reliable recovery, or distributed saga semantics.

## Invariants And Guard Rails

- This package is for synchronous in-process rollback coordination, not for durable workflow orchestration.
- A saga handler defines an ordered list of actions. `Run()` executes them in order on the forward path.
- If an action fails, previously completed actions are compensated in reverse order when a compensation function is defined.
- The original action failure is the error returned to the caller, even if a later compensation step also fails.
- Actions and compensations are typically bound receivers so saga steps can share state accumulated during the workflow.
- Compensation is optional per action. Callers must be explicit about which state changes can and cannot be unwound.

## Caveats

- Cleanup is best-effort only. There is no durable saga log, resumability, retry policy, or asynchronous recovery.
- If a compensation step fails, the package logs the compensation failure and returns the original action error. Recovery may therefore be incomplete and require manual cleanup.
- The implementation is deliberately minimal. It does not attempt to classify transient versus terminal errors or make policy decisions about retries.
- This package does not make a distributed operation atomic. It only provides a structured local pattern for attempting rollback after partial failure.
