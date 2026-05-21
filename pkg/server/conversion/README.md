# pkg/server/conversion

## Intention

`pkg/server/conversion` is the canonical shared conversion layer for the generic part of API handler conversion. Service handlers still own type-specific conversion for their domain objects, but they should build the common resource envelope through this package rather than reimplementing shared metadata, status, scoping, and tag handling locally.

This package bridges the repository's shared metadata vocabulary from [pkg/constants](../constants/README.md) and shared API resource types from [pkg/openapi](../openapi/README.md) into concrete handler conversion code.

It combines four closely related responsibilities:

- projecting shared Kubernetes object metadata into common API resource metadata for read paths
- translating common internal provisioning and health condition state into public API status enums
- assembling and mutating shared Kubernetes object metadata for write paths
- converting shared tag representations between Kubernetes and OpenAPI forms, and logging update patches for debugging

## Invariants And Guard Rails

- This package covers the generic conversion layer only. Service-specific converters must still handle domain fields and resource-specific semantics on top.
- `ResourceReadMetadata`, `OrganizationScopedResourceReadMetadata`, and `ProjectScopedResourceReadMetadata` are the standard way to build the shared API resource envelope from Kubernetes objects.
- `NewObjectMetadata` is the standard base path for constructing shared object metadata from API write metadata when a service needs to create a new Kubernetes resource. Callers are expected to layer scoping and resource-specific labels on top with the builder methods.
- `NewDeterministicObjectMetadata` is the alternative constructor for resources whose Kubernetes name must be derived deterministically from caller-supplied invariant data rather than randomly allocated. It uses UUID v5 (SHA-1); if the first hash does not start with a letter, the previous UUID's bytes are rehashed iteratively until the constraint is met. Fallbacks operate in binary UUID space rather than the invariant string space, so no two distinct invariants can ever produce the same name. A second API create with the same invariant always collides with the first and is rejected with a Kubernetes 409, providing conflict detection without a read-before-write. Each resource type must supply its own fixed namespace UUID constant to prevent cross-type collisions; the invariant must be composed of stable, immutable fields.
- `UpdateObjectMetadata` is the common path for applying shared metadata mutation behavior on update, including the modified timestamp annotation. It is intentionally composable and callers commonly provide additional service-specific mutators on top of the generic behavior.
- Provisioning and health status mapping here is repository-specific policy based on Unikorn status conditions. Callers should not improvise their own generic status mapping for the same resource envelope.
- Deletion takes precedence for provisioning state. If a resource is being deleted, the public provisioning status is reported as `deprovisioning` immediately.
- Tag conversion helpers here are the shared bridge between Kubernetes tag lists and OpenAPI tag lists. Type-specific converters should reuse them rather than duplicating field-by-field translation.

## Caveats

- The package centralizes generic conversion logic, but it still mixes read projection, write metadata assembly, tag translation, and update logging in one place.
- The scoped read-metadata helpers use JSON marshal and unmarshal to copy the shared base metadata into larger generated OpenAPI structs. That is pragmatic generated-type glue rather than an especially elegant conversion model.
- Generic metadata extraction is intentionally tolerant. Missing labels or annotations usually degrade to empty or absent API fields rather than producing hard conversion failures, which means scoping or attribution data can disappear from the outward API view without this layer rejecting the conversion.
- The status mapping is only as expressive as the shared condition vocabulary. Resources with richer or different lifecycle semantics still need service-specific conversion logic around this generic layer.
- `LogUpdate` is a debugging aid that logs a merge-patch-style diff of the resource update. It is useful for visibility, but it is not a patch application mechanism or a general audit system.
