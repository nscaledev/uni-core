/*
Copyright 2026 Nscale.

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

package internal

import (
	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/openapi"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConvertStatusCondition translates from Kubernetes status conditions to API ones.
func ConvertStatusCondition(in metav1.Object) openapi.ResourceProvisioningStatus {
	// We set the status after a reconcile, so this allows us to
	// reflect the correct state to the user immediately.
	if in.GetDeletionTimestamp() != nil {
		return openapi.ResourceProvisioningStatusDeprovisioning
	}

	// Not a resource with status conditions, consider it provisioned.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceProvisioningStatusProvisioned
	}

	// No condition yet, it's unknown.
	condition, err := reader.StatusConditionRead(unikornv1.ConditionAvailable)
	if err != nil {
		return openapi.ResourceProvisioningStatusUnknown
	}

	//nolint:exhaustive
	switch condition.Reason {
	case unikornv1.ConditionReasonProvisioning:
		return openapi.ResourceProvisioningStatusProvisioning
	case unikornv1.ConditionReasonProvisioned:
		return openapi.ResourceProvisioningStatusProvisioned
	case unikornv1.ConditionReasonErrored:
		return openapi.ResourceProvisioningStatusError
	case unikornv1.ConditionReasonDeprovisioning:
		return openapi.ResourceProvisioningStatusDeprovisioning
	}

	return openapi.ResourceProvisioningStatusUnknown
}

// ConvertHealthCondition translates from Kubernetes heath conditions to API ones.
func ConvertHealthCondition(in any) openapi.ResourceHealthStatus {
	// Not a resource with status conditions, consider it healthy.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceHealthStatusHealthy
	}

	// No condition yet, it's unknown.
	condition, err := reader.StatusConditionRead(unikornv1.ConditionHealthy)
	if err != nil {
		return openapi.ResourceHealthStatusUnknown
	}

	//nolint:exhaustive
	switch condition.Reason {
	case unikornv1.ConditionReasonHealthy:
		return openapi.ResourceHealthStatusHealthy
	case unikornv1.ConditionReasonDegraded:
		return openapi.ResourceHealthStatusDegraded
	case unikornv1.ConditionReasonErrored:
		return openapi.ResourceHealthStatusError
	}

	return openapi.ResourceHealthStatusUnknown
}
