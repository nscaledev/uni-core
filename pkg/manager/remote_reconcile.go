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
	"errors"

	"github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	kerrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// RemoteReconciler is a generic reconciler for all manager types.
// The key difference between this an the base reconciler is that the base is
// for types managed by that package, whereas this is for types managed by another.
// This is used primarily as a Kubernetes interface that provides a message queue
// like function e.g. a compute cluster controller can react to project life cycle
// events initiated by the identity service.
type RemoteReconciler struct {
	// options allows CLI options to be interrogated in the reconciler.
	options *options.Options

	// manager grants access to things like clients and eventing.
	manager manager.Manager

	// createProvisioner provides a type agnosic method to create a root provisioner.
	createProvisioner ProvisionerCreateFunc

	// controllerOptions are options to be passed to the reconciler.
	controllerOptions ControllerOptions
}

// NewRemoteReconciler creates a new reconciler.
func NewRemoteReconciler(options *options.Options, controllerOptions ControllerOptions, manager manager.Manager, createProvisioner ProvisionerCreateFunc) *RemoteReconciler {
	return &RemoteReconciler{
		options:           options,
		manager:           manager,
		createProvisioner: createProvisioner,
		controllerOptions: controllerOptions,
	}
}

// Ensure this implements the reconcile.Reconciler interface.
var _ reconcile.Reconciler = &Reconciler{}

// Reconcile is the top-level reconcile interface that controller-runtime will
// dispatch to.  It initialises the provisioner, extracts the request object and
// based on whether it exists or not, reconciles or deletes the object respectively.
func (r *RemoteReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	provisioner := r.createProvisioner(r.controllerOptions)

	object := provisioner.Object()

	// Add the manager to grant access to eventing.
	ctx = NewContext(ctx, r.manager)

	// The namespace allows access to the current namespace to lookup any
	// namespace scoped resources.
	ctx = client.NewContextWithNamespace(ctx, r.options.Namespace)

	// The static client is used by the application provisioner to get access to
	// application bundles and definitions regardless of remote cluster scoping etc.
	ctx = client.NewContextWithProvisionerClient(ctx, r.manager.GetClient())

	// See if the object exists or not, if not it's been deleted.
	if err := r.manager.GetClient().Get(ctx, request.NamespacedName, object); err != nil {
		if kerrors.IsNotFound(err) {
			log.Info("object deleted")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	// If it's being deleted, ignore if there are no finalizers, Kubernetes is in
	// charge now.  If the finalizer is still in place, run the deprovisioning.
	if object.GetDeletionTimestamp() != nil {
		if len(object.GetFinalizers()) == 0 {
			return reconcile.Result{}, nil
		}

		log.Info("deleting object")

		return r.reconcileDelete(ctx, provisioner)
	}

	// Create or update the resource.
	log.Info("reconciling object")

	return r.reconcileNormal(ctx, provisioner)
}

// reconcileDelete handles object deletion.
func (r *RemoteReconciler) reconcileDelete(ctx context.Context, provisioner provisioners.Provisioner) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	if err := provisioner.Deprovision(ctx); err != nil {
		if !errors.Is(err, provisioners.ErrYield) {
			log.Error(err, "deprovisioning failed unexpectedly")
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	return reconcile.Result{}, nil
}

// reconcileNormal adds the application finalizer, provisions the resource and
// updates the resource status to indicate progress.
func (r *RemoteReconciler) reconcileNormal(ctx context.Context, provisioner provisioners.Provisioner) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	if err := provisioner.Provision(ctx); err != nil {
		if !errors.Is(err, provisioners.ErrYield) {
			log.Error(err, "provisioning failed unexpectedly")
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	return reconcile.Result{}, nil
}
