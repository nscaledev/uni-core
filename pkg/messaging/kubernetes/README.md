# pkg/messaging/kubernetes

## Intention

`pkg/messaging/kubernetes` is the current in-tree backend for
[pkg/messaging](../README.md). It makes the queue contract work by wrapping
controller-runtime manager/controller machinery around one watched Kubernetes
object type and translating reconcile events into `messaging.Envelope` deliveries.

This is a real backend, but it is not evidence of a mature multi-backend queue
abstraction. Today the abstraction has one implementation, and that implementation
is essentially controller-runtime reconciliation dressed up as a queue.

## What Lives Here

- `MessageQueue`, which owns:
  - manager construction and leader election when run standalone
  - watch registration for one object type
  - in-process fan-out to registered consumers
- `Run()`, which starts the manager and controller.
- `SetupWithManager()`, which registers the controller with an existing
  controller-runtime manager.
- `Reconcile()`, which loads the watched object and converts it into the minimal
  `messaging.Envelope` understood by consumers.

## Relationships

- [pkg/messaging](../README.md) defines the replay/retry contract this backend is
  expected to satisfy.
- [pkg/messaging/consumer](../consumer/README.md) contains the current main
  consumer pattern this backend drives.

## Invariants

- This backend watches exactly one Kubernetes object type per queue instance.
- Leader election is always enabled because Kubernetes does not partition work like
  an external message broker would.
- Delivery semantics come from controller-runtime reconciliation:
  - active objects are replayed by informer/controller startup behavior
  - consumer failure causes reconcile failure and therefore retry
- The emitted envelope is intentionally sparse: resource name plus optional
  deletion timestamp. Consumers are expected to rehydrate real state from the
  system of record.

## Caveats

- This is not an independent queue model. It is a controller-runtime wrapper with
  queue-like semantics.
- Only one backend exists today. Future Kafka/NATS-style backends are intended by
  the abstraction but have not yet pressure-tested it.
