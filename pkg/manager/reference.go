/*
Copyright 2025 the Unikorn Authors.
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

package manager

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
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

// resourceIDMap takes a resource list and generates a map from resource ID to the
// resource itself.
func resourceIDMap(resources client.ObjectList) (map[string]client.Object, error) {
	out := map[string]client.Object{}

	callback := func(resource runtime.Object) error {
		object, ok := resource.(client.Object)
		if !ok {
			return fmt.Errorf("%w: resource not a client object", errors.ErrTypeConversion)
		}

		out[object.GetName()] = object

		return nil
	}

	if err := meta.EachListItem(resources, callback); err != nil {
		return nil, err
	}

	return out, nil
}

// AddResourceReference adds the given resource reference to the specified resource.
// This is typically run by a controller before the resource is consumed.
func AddResourceReference(ctx context.Context, cli client.Client, resource client.Object, key client.ObjectKey, reference string) error {
	log := log.FromContext(ctx)

	if err := cli.Get(ctx, key, resource); err != nil {
		return err
	}

	if updated := controllerutil.AddFinalizer(resource, reference); !updated {
		return nil
	}

	if log.V(1).Enabled() {
		log.Info("adding resource reference", "reference", reference, "id", key.Name, "type", reflect.ValueOf(resource).Elem().Type().Name())
	}

	if err := cli.Update(ctx, resource); err != nil {
		if kerrors.IsConflict(err) {
			return provisioners.ErrYield
		}

		return err
	}

	return nil
}

// AddResourceReferences adds the given resource reference to all resources that match the selector
// and that are in the given set of IDs.  An error is raised if an ID is not present.  This is
// typically run by a controller before the resource is consumed.
func AddResourceReferences(ctx context.Context, cli client.Client, resources client.ObjectList, options *client.ListOptions, reference string, ids []string) error {
	log := log.FromContext(ctx)

	if err := cli.List(ctx, resources, options); err != nil {
		return err
	}

	resourceIDMap, err := resourceIDMap(resources)
	if err != nil {
		return err
	}

	for _, id := range ids {
		resource, ok := resourceIDMap[id]
		if !ok {
			return fmt.Errorf("%w: attempt to reference unknown resource ID %s", errors.ErrConsistency, id)
		}

		if updated := controllerutil.AddFinalizer(resource, reference); !updated {
			continue
		}

		if log.V(1).Enabled() {
			log.Info("adding resource reference", "reference", reference, "id", id, "type", reflect.ValueOf(resource).Elem().Type().Name())
		}

		if err := cli.Update(ctx, resource); err != nil {
			if kerrors.IsConflict(err) {
				return provisioners.ErrYield
			}

			return err
		}
	}

	return nil
}

// RemoveResourceReference removes the given resource reference from the selected resource.
// This is typically run by a controller after a resource has stopped being used.
func RemoveResourceReference(ctx context.Context, cli client.Client, resource client.Object, key client.ObjectKey, reference string) error {
	log := log.FromContext(ctx)

	if err := cli.Get(ctx, key, resource); err != nil {
		return err
	}

	if updated := controllerutil.RemoveFinalizer(resource, reference); !updated {
		return nil
	}

	if log.V(1).Enabled() {
		log.Info("removing resource reference", "reference", reference, "id", key.Name, "type", reflect.ValueOf(resource).Elem().Type().Name())
	}

	if err := cli.Update(ctx, resource); err != nil {
		if kerrors.IsConflict(err) {
			return provisioners.ErrYield
		}

		return err
	}

	return nil
}

// RemoveResourceReferences removes the given resource reference from all resources that match the
// selector and that are not in the given set of IDs.  This is typically run by a controller after
// a resource has stopped being used.
func RemoveResourceReferences(ctx context.Context, cli client.Client, resources client.ObjectList, options *client.ListOptions, reference string, ids []string) error {
	log := log.FromContext(ctx)

	if err := cli.List(ctx, resources, options); err != nil {
		return err
	}

	callback := func(resource runtime.Object) error {
		object, ok := resource.(client.Object)
		if !ok {
			return fmt.Errorf("%w: resource not a client object", errors.ErrTypeConversion)
		}

		if slices.Contains(ids, object.GetName()) {
			return nil
		}

		if updated := controllerutil.RemoveFinalizer(object, reference); !updated {
			return nil
		}

		if log.V(1).Enabled() {
			log.Info("removing resource reference", "reference", reference, "id", object.GetName(), "type", reflect.ValueOf(resource).Elem().Type().Name())
		}

		if err := cli.Update(ctx, object); err != nil {
			if kerrors.IsConflict(err) {
				return provisioners.ErrYield
			}

			return err
		}

		return nil
	}

	return meta.EachListItem(resources, callback)
}

// ClearResourceReferences is used by controllers whose object may reference one of
// many other resources e.g. a server can reference multiple security groups.  This
// is used to clean them out during the finalizing phase of deletion.
func ClearResourceReferences(ctx context.Context, cli client.Client, resources client.ObjectList, options *client.ListOptions, reference string) error {
	return RemoveResourceReferences(ctx, cli, resources, options, reference, nil)
}
