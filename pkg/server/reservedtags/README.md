# pkg/server/reservedtags

## Intention

Lets an operator reserve tag prefixes so that only callers holding a named RBAC permission can write tags under them. A managed service that records what a resource belongs to in tags uses this to stop users changing or deleting those tags by accident.

UNI reserves nothing. Every prefix is deployment configuration, set with `--reserved-tag-prefixes=<prefix>=<permission>,...`. The default is empty, which leaves behaviour unchanged.

The permission is an Identity RBAC permission name, what Identity's roles and ACLs call an endpoint, for example `example:tags`. It is a free string: it needs no registering, and a caller holds it once a role bound to them lists it with the operation. For example, `--reserved-tag-prefixes=example.org/=example:tags` with a role granting `example:tags: [create, update]`.

## Rules

- A tag is reserved when its name starts with a configured prefix, compared under Unicode case folding. Stored names are never rewritten.
- The caller owns a prefix when it holds the prefix's permission for the operation being served (`create` on create, `update` on update) at the resource's scope. Each prefix is judged on its own.
- `OnCreate`: a reserved tag the caller does not own refuses the whole request with 403, naming the tag.
- `OnUpdate`: under a prefix the caller owns, the request decides, so omitting a tag removes it. Under one it does not own, the stored tags are kept and the request is ignored, with no error.
- Reads are unchanged.

## Changing The Configuration

Stored tags are never rewritten, so changing the flag changes only who may write them from then on.

- **Adding a prefix.** Existing tags under it stay as they are. Non-owners can no longer change or remove them, and a create that carries one is refused. A tag a user set by hand before the prefix was reserved becomes frozen for that user; only an owner, or a direct Kubernetes write, can change it. Scan for such tags before reserving. Grant the owner its permission, for both `create` and `update`, first: without `create` its creates are refused, and without `update` its tag changes are ignored silently.
- **Removing a prefix.** Its tags become ordinary user tags again, which anyone who may update the resource can change or delete. Nothing is rewritten, so removing a prefix is the rollback.
- **Changing a prefix's permission.** Ownership moves to the new permission's holders.

A change takes effect when the server restarts with the new flag. During a rolling update, old and new replicas briefly disagree. Each service enforces only its own flag, so a prefix reserved on one service and not another is protected only on the first.

## Invariants And Guard Rails

- Call `Validate` at startup. It refuses empty prefixes, empty permissions, and prefixes where one starts with another, so a tag matches at most one prefix.
- The service supplies `Holds`, because core cannot depend on identity. It must not record a refusal: a false answer is normal on every non-owner update.
- Call `OnUpdate` with the stored tags of the version being written, under an optimistic lock, or a non-owner's write can restore a value the owner just changed.
- Every handler that writes tags in a service configured with a prefix must call the helper. A handler that does not is unprotected, silently.
- Only API handlers are covered. Controllers and direct Kubernetes writes bypass it.
