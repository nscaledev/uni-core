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
	"fmt"

	"github.com/unikorn-cloud/core/pkg/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetResourceReference takes a resource and generates a unique reference for use with
// blocking deletion.
func GetResourceReference(client client.Client, resource client.Object) (string, error) {
	gvks, _, err := client.Scheme().ObjectKinds(resource)
	if err != nil {
		return "", err
	}

	if len(gvks) != 1 {
		return "", fmt.Errorf("%w: expected single GVK for resource", errors.ErrConsistency)
	}

	gvk := gvks[0]

	mapping, err := client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", err
	}

	gvr := mapping.Resource

	// TODO: This is a legacy thing where the OG Kubernetes service is not corrcetly
	// namespaced, much like Amazon regions :D  We really need to work out a migration
	// strategy.
	if gvr.Group == "unikorn-cloud.org" {
		gvr.Group = "kubernetes.unikorn-cloud.org"
	}

	return fmt.Sprintf("%s.%s/%s", gvr.Resource, gvr.Group, resource.GetName()), nil
}
