# pkg/provisioners/util

## Intention

`pkg/provisioners/util` is a small helper package for common provisioner chores. It is not a broad abstraction layer for provisioners, and it only groups together a few pieces that ended up being reused across multiple provisioner implementations.

The package currently covers two narrow themes:

- control-plane scheduling fragments used when provisioners need to render workload placement policy into Helm values or similar configuration
- a couple of generic helper functions used by provisioners for namespace lookup and configuration change detection

## Invariants And Guard Rails

- The control-plane scheduling helpers are intentionally untyped. They return `[]any` and `map[string]any` because they are meant to feed rendered configuration and chart values, not to construct typed Kubernetes API objects directly in Go.
- `ControlPlaneTolerations()` and `ControlPlaneNodeSelector()` define the repository's standard scheduling fragments for forcing workloads onto control-plane nodes when managed-service placement requires that.
- `ControlPlaneInitTolerations()` adds the extra bootstrap-time tolerations needed for components such as CNIs or cloud-provider integrations that must run before the cluster is fully initialized.
- `GetResourceNamespace()` is the common helper for provisioner paths that expect a label selector to resolve to exactly one namespace.
- `GetConfigurationHash()` is the common helper for deriving a stable hash from rendered configuration so callers can trigger restarts for workloads that do not react cleanly to configuration changes.

## Caveats

- The package boundary is mixed. Scheduling policy fragments, namespace lookup, and configuration hashing are only loosely related.
- The control-plane scheduling helpers encode repository policy, not universal Kubernetes best practice. They are appropriate where the platform deliberately wants managed components on control-plane nodes, including scale-to-zero worker scenarios.
- `GetResourceNamespace()` treats anything other than exactly one matching namespace as an error. That is convenient for callers that rely on uniqueness, but it means the helper is only valid where the label contract is already strong.
- `GetConfigurationHash()` is operational workaround logic. It exists because some managed applications do not respond properly to configuration changes and need a forced restart signal.
