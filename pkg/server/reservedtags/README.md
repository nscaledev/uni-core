# pkg/server/reservedtags

## Intention

Lets an operator reserve tag prefixes so that only callers holding a named RBAC endpoint can write tags under them. A managed service that records what a resource belongs to in tags uses this to stop users changing or deleting those tags by accident.

UNI reserves nothing. Every prefix is deployment configuration, set with `--reserved-tag-prefixes=<prefix>=<endpoint>,...`. The default is empty, which leaves behaviour unchanged.

## Rules

- A tag is reserved when its name starts with a configured prefix, compared under Unicode case folding. Stored names are never rewritten.
- The caller owns a prefix when it holds the prefix's endpoint for the operation being served (`create` on create, `update` on update) at the resource's scope. Each prefix is judged on its own.
- `OnCreate`: a reserved tag the caller does not own refuses the whole request with 403, naming the tag.
- `OnUpdate`: under a prefix the caller owns, the request decides, so omitting a tag removes it. Under one it does not own, the stored tags are kept and the request is ignored, with no error.
- Reads are unchanged.

## Invariants And Guard Rails

- Call `Validate` at startup. It refuses empty prefixes, empty endpoints, and prefixes where one starts with another, so a tag matches at most one prefix.
- The service supplies `Holds`, because core cannot depend on identity. It must not record a refusal: a false answer is normal on every non-owner update.
- Call `OnUpdate` with the stored tags of the version being written, under an optimistic lock, or a non-owner's write can restore a value the owner just changed.
- Every handler that writes tags in a service configured with a prefix must call the helper. A handler that does not is unprotected, silently.
- Only API handlers are covered. Controllers and direct Kubernetes writes bypass it.
