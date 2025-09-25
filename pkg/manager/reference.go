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
	"fmt"
	"slices"

	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/errors"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// GenerateResourceReference takes a resource and generates a unique reference for use with
// blocking deletion.
func GenerateResourceReference(client client.Client, resource client.Object) (string, error) {
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

// GetResourceReferences returns all resource references attached to a resource.
// This is used primarily to poll a resource to see if it's in use, and thus its
// deletion will have consequences.  It may also be used to inhibit deletion in
// certain cercumstances.
func GetResourceReferences(object client.Object) []string {
	ignored := []string{
		// Our finalizer to inhibit deletion until we are finished.
		constants.Finalizer,
		// Some internal components will use cacscading deletion to
		// block deletion.
		metav1.FinalizerDeleteDependents,
	}

	discard := func(s string) bool {
		return slices.Contains(ignored, s)
	}

	return slices.DeleteFunc(slices.Clone(object.GetFinalizers()), discard)
}

// ClearResourceReferences is used by controllers whose object may reference one of
// many other resources e.g. a server can reference multiple security groups.  This
// is used to clean them out during the finalizing phase of deletion.
func ClearResourceReferences(ctx context.Context, cli client.Client, resources client.ObjectList, options *client.ListOptions, reference string) error {
	if err := cli.List(ctx, resources, options); err != nil {
		return err
	}

	callback := func(resource runtime.Object) error {
		object, ok := resource.(client.Object)
		if !ok {
			return fmt.Errorf("%w: resource not a client object", errors.ErrTypeConversion)
		}

		if updated := controllerutil.RemoveFinalizer(object, reference); !updated {
			return nil
		}

		return cli.Update(ctx, object)
	}

	return meta.EachListItem(resources, callback)
}
