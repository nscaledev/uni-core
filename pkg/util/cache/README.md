# pkg/util/cache

## Intention

`pkg/util/cache` is not one coherent cache abstraction. It is a utility package that currently contains several cache strategies with different cost and correctness tradeoffs.

The main reason this package exists today is `RefreshAheadCache`, which is the repository's higher-performance cache for indexed sets of resources where request-path refresh latency and visibility guarantees matter. The smaller `TimeoutCache` and `LRUExpireCache` helpers are still live and useful, but they solve simpler problems and do not share the same model.

The current cache families are:

- `TimeoutCache` for a single cached value with a timeout and explicit invalidation
- `LRUExpireCache` for typed LRU+TTL storage over `apimachinery`'s cache, with defensive deep-copy semantics by default
- `RefreshAheadCache` for background-refreshed indexed snapshots, synchronous invalidation, epoch-based snapshot identity, and local write-through overlay behavior

## Invariants And Guard Rails

- Choose the cache type for its operational model, not just for convenience. These types do not implement interchangeable caching semantics.
- `TimeoutCache` is the simple TTL/invalidate model. Once the value expires or is invalidated, the next caller that needs fresh data must pay the refresh cost.
- `RefreshAheadCache` exists to avoid pushing that refresh cost onto normal read paths. `Run()` performs an initial blocking load, then keeps the cache warm with periodic refresh.
- `RefreshAheadCache.Invalidate()` is deliberately synchronous. On success, callers can assume the refreshed data is visible in that cache instance before control returns.
- `RefreshAheadCache` is designed around uniquely indexed sets of resources and a single cache instance. Its correctness model is not a distributed coherence protocol.
- `RefreshAheadCache` local write-through helpers rely on a strict usage rule: the corresponding backend write must already have committed synchronously and atomically before the cache is updated locally.
- `RefreshAheadCache` epochs describe the identity of the visible cache snapshot. Callers may memoize derived work against an epoch and reuse it until that epoch changes.
- `LRUExpireCache` defaults to deep-copy behavior to reduce accidental mutation of cached values. `ZeroCopy()` is an explicit tradeoff that gives speed back to the caller at the cost of safety.

## Caveats

- The package boundary is broad and a little ugly. `TimeoutCache`, `LRUExpireCache`, and `RefreshAheadCache` are related only in the loose sense that they all cache things.
- `TimeoutCache` is simple but leaves refresh ownership with whoever reads after expiry or invalidation. That pushes refresh latency onto the unlucky reader, which is the main problem `RefreshAheadCache` is intended to solve for hotter read paths.
- `RefreshAheadCache` is sophisticated enough that correctness depends on usage discipline, not just on calling the exported methods. The overlay model only protects local writes against refreshes already in flight; it is not a long-lived reconciliation layer.
- `RefreshAheadCache` assumes a single writer-view per cache instance. If one process writes and another process reads through a different cache instance, read-your-writes is not guaranteed.
- `RefreshAheadCache` is optimized for pointer-based zero-copy reads and snapshot reuse. That helps performance, but it means callers need to understand that individual items are shared references, not defensive deep copies.
- `Invalidate()` only becomes operational after `Run()` has initialized the refresh loop. Calling it before startup can block indefinitely waiting for a refresh channel that does not exist yet. This is a real lifecycle hazard, not a graceful mode.
- `LRUExpireCache.Add()` and `Get()` silently degrade on deep-copy failure by dropping the value or returning a miss.
- `TimeoutCache.Invalidate()` currently mutates cache state without taking the same lock used by `Get()` and `Set()`. That is an implementation wart, not a design feature.
