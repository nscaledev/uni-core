# pkg/errors

## Intention

`pkg/errors` is the platform's canonical shared sentinel-error taxonomy. It exists to give code across this repository and sibling repositories one common set of coarse-grained error categories that can be wrapped with local detail and still recognized with `errors.Is(...)`.

This package is not trying to model rich typed errors. Its role is to define shared category roots for recurring platform-level failure classes such as invalid context wiring, kubeconfig problems, malformed secrets, unexpected API status codes, internal consistency failures, unsupported key types, missing required keys, type conversion failures, resource-not-found conditions, and conflicts.

The intended usage pattern is to wrap these sentinels with contextual detail using `%w` at the point of failure, then branch on the shared category at higher layers when behavior needs to differ.

## Invariants And Guard Rails

- This is the canonical shared sentinel set for the platform. If a category already exists here, code should reuse it rather than define another local sentinel with the same meaning.
- These errors are for coarse-grained classification and control flow, not for carrying all user-facing detail by themselves.
- Callers should preserve sentinel identity. Prefer wrapping with `%w` to add debugging context while keeping `errors.Is(...)` working across package boundaries.
- The names here represent cross-cutting failure categories, not package-local implementation details.
- `ErrConsistency` is for internal consistency failures, violated assumptions, and invariant-style errors inside the platform, not as a substitute for ordinary validation or user error reporting.
- `ErrResourceNotFound` and `ErrConflict` are shared semantic categories intended to support common not-found and conflict handling across services.

## Caveats

- Canonical in intent does not mean canonical everywhere in practice yet. Some packages and sibling repositories still carry duplicate local sentinels for categories that already exist here, and those duplicates should be consolidated back onto this package as they are found.
- That consolidation is an explicit cleanup action item, not a cosmetic preference. Duplicate generic sentinels weaken `errors.Is(...)`-based behavior and fragment the platform's error taxonomy.
- This package is for generic cross-cutting error categories. Errors that are genuinely specific to a narrower domain or subsystem, such as a cache-specific conflict or a JOSE-specific key-format problem, should stay local to the package that owns that behavior.
- The taxonomy here is intentionally shallow. It is useful for broad branching and classification, but many callers still need additional wrapped context to make an error actionable.
- Some names are broad by design. That keeps reuse simple, but it also means callers must be disciplined about adding precise context rather than returning these sentinels naked.
