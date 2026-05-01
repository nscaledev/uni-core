# pkg/messaging

## Intention

`pkg/messaging` defines a narrow message-queue abstraction for resource lifecycle events. Its job is to let platform code consume replayable resource messages without coupling directly to a specific backend implementation.

The key semantic is replayable lifecycle delivery. Implementations must be able to replay all currently active resources on startup so consumers can re-witness state after restarts and continue any required work that might otherwise have been missed.

The main proven use case for that replay behavior today is deletion recovery. Deletion in the platform is often reference-driven rather than immediate. A resource such as a project may remain blocked from deletion while other resources still reference it. Messaging consumers are used to fan deletion outward, remove dependents, and allow those references to clear so deletion can complete eventually.

The abstraction is intended to support backends such as Kubernetes today and
systems such as Kafka or NATS later. The current envelope is deliberately small:
it carries a resource ID and optional deletion timestamp, and consumers are
expected to rehydrate any richer state they need from the system of record.

See [kubernetes](./kubernetes/README.md) for the current backend and
[consumer](./consumer/README.md) for the current reusable consumer pattern.

## Invariants And Guard Rails

- This package is a resource-lifecycle messaging abstraction, not a general-purpose event bus API.
- Queue implementations must replay all active resources on startup so consumers can recover missed work after restarts.
- If a consumer returns an error, the queue implementation must requeue or retry the event rather than treating it as successfully handled.
- Consumers should be written to tolerate replay and repeated delivery. The contract assumes recovery and retries, not exactly-once processing.
- The envelope is intentionally minimal. Consumers should derive any richer state they need from the resource ID and the system of record rather than expecting a full event payload here.
- Deletion is the most important currently proven semantic carried by this abstraction. A nil deletion timestamp routes the message as a live or non-deleting resource event; a populated deletion timestamp means deletion fan-out or other cleanup logic may need to run.

## Caveats

- The abstraction is broader in intent than in its current in-tree implementation. Today there is only one backend, and it is effectively controller-runtime reconciliation wrapped in a queue-shaped interface.
- The current Kubernetes backend is tightly coupled to controller-runtime manager and reconcile mechanics, so future non-Kubernetes backends are intended but not yet pressure-tested by this abstraction.
- The envelope is intentionally sparse. That keeps queue semantics simple, but it also limits this abstraction to consumers that can rehydrate needed state from the system of record.
- The main proven use case today is deletion fan-out and cascading cleanup. If future consumers expect richer event semantics, this package contract may need to grow or split.
