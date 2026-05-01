# pkg

## Intention

`pkg` is the shared platform library surface of this repository. It is not one
framework. It is the collection of cross-cutting contracts, runtime layers, and
historical baggage that sibling services and controllers depend on.

The useful way to read it is as a small knowledge graph:

- API/object contract:
  [apis](./apis/README.md), [openapi](./openapi/README.md),
  [constants](./constants/README.md), [errors](./errors/README.md)
- Runtime/bootstrap and client layers:
  [options](./options/README.md), [client](./client/README.md)
- Reconciler/controller stack:
  [provisioners](./provisioners/README.md), [manager](./manager/README.md),
  [messaging](./messaging/README.md)
- Server/API stack:
  [server](./server/README.md)
- Utilities and support layers:
  [util](./util/README.md)

## Reading Order

- Start with [constants](./constants/README.md),
  [errors](./errors/README.md), [apis/unikorn/v1alpha1](./apis/unikorn/v1alpha1/README.md),
  and [openapi](./openapi/README.md) for the shared contract vocabulary.
- Then read [client](./client/README.md) and [options](./options/README.md) for
  process bootstrap and client construction.
- controllers and provisioners:
  [provisioners](./provisioners/README.md) -> [manager](./manager/README.md)
- service APIs:
  [server](./server/README.md)

## Caveats

- This tree contains both deliberately shared platform layers and a fair amount of
  historical aggregation. Several packages are catch-alls that should eventually
  split rather than grow.
- Some subtrees are intentionally de-emphasized in this documentation pass:
  `pkg/cd` because it is legacy in-tree CD surface, and most of `pkg/testing`
  because it is test support rather than core runtime contract.
