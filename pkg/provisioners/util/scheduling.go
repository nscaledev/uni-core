/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

// ControlPlaneTolerations returns a list of tolerations required to
// have a pod scheduled on the control plane.  This is typically used
// for managed services to keep them off the worker nodes and allow
// scale to zero.
func ControlPlaneTolerations() []any {
	return []any{
		map[string]any{
			"key":    "node-role.kubernetes.io/control-plane",
			"effect": "NoSchedule",
		},
	}
}

// ControlPlaneNodeSelector returns a key/value map of labels to match
// in order to force scheduling on the control plane.  Used in conjunction
// with, and for the same reason as, ControlPlaneTolerations.
func ControlPlaneNodeSelector() map[string]any {
	return map[string]any{
		"node-role.kubernetes.io/control-plane": "",
	}
}

// ControlPlaneInitTolerations are any other tolerate any other taints we
// put in place, or are placed there by the system, on initial control plane
// provisioning to ensure correct operation.  This is typically only for
// things like the CNI and cloud provider.
func ControlPlaneInitTolerations() []any {
	return []any{
		map[string]any{
			"key":    "node.cloudprovider.kubernetes.io/uninitialized",
			"effect": "NoSchedule",
			"value":  "true",
		},
		map[string]any{
			"key":    "node.cilium.io/agent-not-ready",
			"effect": "NoSchedule",
			"value":  "true",
		},
	}
}
