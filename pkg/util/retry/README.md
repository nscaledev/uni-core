# pkg/util/retry

## Intention

`pkg/util/retry` is a minimal polling-style retry helper. It is not a general retry-policy framework. Its job is to keep retrying a callback until it succeeds or until the caller's context is cancelled.

This package exists largely for historical convenience. It is useful for simple standalone waits and short polling loops, but it is not the platform's preferred model for controller reconciliation progress.

It currently provides:

- `Forever()` to construct a retrier with a fixed one-second retry period
- `Do()` and `DoWithContext()` to run the callback immediately and then keep retrying until success or cancellation
- `retry.Error` to preserve both the context cancellation error and the last callback failure when the loop exits because the context ended

## Invariants And Guard Rails

- The callback is invoked immediately before any waiting occurs.
- If the callback continues to fail, retries happen every second until success or context cancellation.
- `Do()` uses `context.TODO()`. `DoWithContext()` is the path to use when cancellation or timeout semantics matter.
- On cancellation, the returned `retry.Error` preserves both why the loop stopped and what the callback was still failing with.
- This helper is acceptable for a small number of simple blocking poll loops where returning control to a framework is not the right model. It should not be treated as the default retry primitive for new workflow code.

## Caveats

- The fixed one-second retry period is historical simplicity, not a carefully tuned repository-wide retry policy.
- This package does not provide backoff, jitter, retry budgets, selective retry by error type, or observability hooks.
- This is not the preferred primitive for controller-runtime reconciler progress. In reconciler code, the platform generally prefers yielding back to controller-runtime and using requeue/retry semantics so reconciliation stays non-blocking and fair.
- Using this helper inside long-running reconciliation paths can work against the fairness and controlled-retry behavior provided elsewhere in the platform.
