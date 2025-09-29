/*
Copyright 2025 the Unikorn Authors.

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

package manager

import (
	"context"

	corev1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceReady reads a resource, and checks its readiness status.
// If available, then it can be used by a child, and it returns nil, otherwise
// returns ErrYield.
func ResourceReady(ctx context.Context, resource corev1.ManagableResourceInterface) error {
	status, err := resource.StatusConditionRead(corev1.ConditionAvailable)
	if err != nil || status.Reason != corev1.ConditionReasonProvisioned {
		log.FromContext(ctx).Info("resource dependency is not ready")

		return provisioners.ErrYield
	}

	return nil
}
