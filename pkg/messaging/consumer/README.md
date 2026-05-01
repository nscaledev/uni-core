# pkg/messaging/consumer

## Intention

`pkg/messaging/consumer` contains reusable consumers for the
[pkg/messaging](../README.md) contract. Today that mostly means one thing:
deletion-driven local fan-out.

The package is not a broad library of message-processing patterns. It is a narrow
home for consumers that can be reused across services when lifecycle events need
to trigger predictable follow-up work.

## What Lives Here

- `CascadingDelete`, a consumer that:
  - ignores live events
  - reacts to deletion events
  - lists local resources, optionally filtered by a resource-ID label
  - issues foreground deletes so downstream cleanup can complete before the
    referenced object is finally removed

## Relationships

- [pkg/messaging](../README.md) defines the envelope, replay, and retry contract.
- [pkg/messaging/kubernetes](../kubernetes/README.md) is the current backend that
  delivers those envelopes.
- [pkg/manager](../../manager/README.md) and the broader finalizer/reference model
  are the reason this consumer exists: deletion of one resource often has to fan
  out before another resource may finish disappearing.

## Invariants

- `CascadingDelete` treats deletion as the only actionable event. A nil deletion
  timestamp is intentionally ignored.
- If `WithResourceLabel()` is used, the consumer assumes that label identifies the
  local resources owned by or referencing the deleted upstream resource.
- Foreground deletion is used on purpose so owner-reference and finalizer-driven
  cleanup blocks until dependents are actually cleared.

## Caveats

- This is a sharp tool. If the resource label is wrong or omitted carelessly, the
  consumer can delete far more than intended.
- The consumer is only thinly generic. It relies on the local resource type being
  listable via `client.ObjectList` and on the system of record being Kubernetes.
- The package currently has one real consumer. If more appear, they should earn
  their place by being genuinely reusable rather than just being nearby.
