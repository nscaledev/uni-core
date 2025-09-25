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

package manager

import (
	"context"
	"errors"
	"fmt"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/cd/argocd"
	"github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	"github.com/unikorn-cloud/core/pkg/provisioners/application"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	// ErrResourceError is raised when this is used with an unsupported resource
	// kind.
	ErrResourceError = errors.New("unable to assert resource type")
)

// ProvisionerCreateFunc provides a type agnosic method to create a root provisioner.
type ProvisionerCreateFunc func(ControllerOptions) provisioners.ManagerProvisioner

// Reconciler is a generic reconciler for all manager types.
type Reconciler struct {
	// options allows CLI options to be interrogated in the reconciler.
	options *options.Options

	// manager grants access to things like clients and eventing.
	manager manager.Manager

	// createProvisioner provides a type agnosic method to create a root provisioner.
	createProvisioner ProvisionerCreateFunc

	// controllerOptions are options to be passed to the reconciler.
	controllerOptions ControllerOptions
}

// NewReconciler creates a new reconciler.
func NewReconciler(options *options.Options, controllerOptions ControllerOptions, manager manager.Manager, createProvisioner ProvisionerCreateFunc) *Reconciler {
	return &Reconciler{
		options:           options,
		manager:           manager,
		createProvisioner: createProvisioner,
		controllerOptions: controllerOptions,
	}
}

// Ensure this implements the reconcile.Reconciler interface.
var _ reconcile.Reconciler = &Reconciler{}

func (r *Reconciler) getDriver() (cd.Driver, error) {
	if r.options.CDDriver.Kind != cd.DriverKindArgoCD {
		return nil, coreerrors.ErrCDDriver
	}

	return argocd.New(r.manager.GetClient(), argocd.Options{}), nil
}

// Reconcile is the top-level reconcile interface that controller-runtime will
// dispatch to.  It initialises the provisioner, extracts the request object and
// based on whether it exists or not, reconciles or deletes the object respectively.
func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	provisioner := r.createProvisioner(r.controllerOptions)

	object := provisioner.Object()

	driver, err := r.getDriver()
	if err != nil {
		return reconcile.Result{}, err
	}

	// Add the manager to grant access to eventing.
	ctx = NewContext(ctx, r.manager)

	// The namespace allows access to the current namespace to lookup any
	// namespace scoped resources.
	ctx = client.NewContextWithNamespace(ctx, r.options.Namespace)

	// The static client is used by the application provisioner to get access to
	// application bundles and definitions regardless of remote cluster scoping etc.
	ctx = client.NewContext(ctx, r.manager.GetClient())

	// The cluster context is updated as remote clusters are descended into.
	clusterContext := &client.ClusterContext{
		// TODO: cluster information.
		Client: r.manager.GetClient(),
	}

	ctx = client.NewContextWithCluster(ctx, clusterContext)

	// The driver context is updated as remote provisioners are descended into.
	ctx = cd.NewContext(ctx, driver)

	// The application context contains a reference to the resource that caused
	// their creation.
	ctx = application.NewContext(ctx, object)

	// See if the object exists or not, if not it's been deleted.
	if err := r.manager.GetClient().Get(ctx, request.NamespacedName, object); err != nil {
		if kerrors.IsNotFound(err) {
			log.Info("object deleted")

			return reconcile.Result{}, nil
		}

		return reconcile.Result{}, err
	}

	if object.Paused() {
		log.Info("reconcilication paused")

		return reconcile.Result{}, nil
	}

	// If it's being deleted, ignore if there are no finalizers, Kubernetes is in
	// charge now.  If the finalizer is still in place, run the deprovisioning.
	if object.GetDeletionTimestamp() != nil {
		if len(object.GetFinalizers()) == 0 {
			return reconcile.Result{}, nil
		}

		log.Info("deleting object")

		return r.reconcileDelete(ctx, provisioner, object)
	}

	// Create or update the resource.
	log.Info("reconciling object")

	return r.reconcileNormal(ctx, provisioner, object)
}

// reconcileDelete handles object deletion.
func (r *Reconciler) reconcileDelete(ctx context.Context, provisioner provisioners.Provisioner, object unikornv1.ManagableResourceInterface) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	// Wait for any owned resources to be cleaned up first.
	if controllerutil.ContainsFinalizer(object, metav1.FinalizerDeleteDependents) {
		log.Info("awaiting owned resource deletion")

		// Still want it to report as deprovisioning.
		// TODO: perhaps a blocked state would be useful??
		if err := r.handleReconcileCondition(ctx, object, provisioners.ErrYield, true); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	// Resources from one service can inhibit the deletion of those in others to
	// enforce deletion ordering.  This is especially relevant for cascading deletion
	// of CAPO Kubernetes clusters via the underlying network...  Deletion of the
	// network will actually trigger deletion of the underlying cloud identity.
	// Identity deletion will trigger network deletion, and block until that's done.
	// Network deletion will trigger cluster deletion (via an event).  If the identity
	// was deleted before the cluster had finished with it there's a very real possibility
	// of errors being displayed to users, hangs (because OpenStack will happily delete a
	// project with compute and network resources still using it, which will get orhpaned
	// and cannot be deleted via non-admin means), or more insiduosly resource leaks.
	if references := GetResourceReferences(object); len(references) > 0 {
		log.Info("awaiting resource reference deletion", "references", references)

		// Still want it to report as deprovisioning.
		// TODO: perhaps a blocked state would be useful??
		if err := r.handleReconcileCondition(ctx, object, provisioners.ErrYield, true); err != nil {
			return reconcile.Result{}, err
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	perr := provisioner.Deprovision(ctx)

	if err := r.handleReconcileCondition(ctx, object, perr, true); err != nil {
		return reconcile.Result{}, err
	}

	// If anything went wrong, requeue for another attempt.
	// NOTE: DO NOT return an error, and use a constant period or you will
	// suffer from an exponential back-off and kill performance.
	if perr != nil {
		if !errors.Is(perr, provisioners.ErrYield) {
			log.Error(perr, "deprovisioning failed unexpectedly")
		}

		log.Info("controller yielding")

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	// All good, signal the resource can be deleted.
	if ok := controllerutil.RemoveFinalizer(object, constants.Finalizer); ok {
		if err := r.manager.GetClient().Update(ctx, object); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// reconcileNormal adds the application finalizer, provisions the resource and
// updates the resource status to indicate progress.
func (r *Reconciler) reconcileNormal(ctx context.Context, provisioner provisioners.Provisioner, object unikornv1.ManagableResourceInterface) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	// Add the finalizer so we can orchestrate resource garbage collection.
	if ok := controllerutil.AddFinalizer(object, constants.Finalizer); ok {
		if err := r.manager.GetClient().Update(ctx, object); err != nil {
			return reconcile.Result{}, err
		}
	}

	perr := provisioner.Provision(ctx)

	// Update the status conditionally, this will remove transient errors etc.
	if err := r.handleReconcileCondition(ctx, object, perr, false); err != nil {
		return reconcile.Result{}, err
	}

	// If anything went wrong, requeue for another attempt.
	// NOTE: DO NOT return an error, and use a constant period or you will
	// suffer from an exponential back-off and kill performance.
	if perr != nil {
		if !errors.Is(perr, provisioners.ErrYield) {
			log.Error(perr, "provisioning failed unexpectedly")
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	return reconcile.Result{}, nil
}

// handleReconcileCondition inspects the error, if any, that halted the provisioning and reports
// this as a ppropriate in the status.
func (r *Reconciler) handleReconcileCondition(ctx context.Context, object unikornv1.ManagableResourceInterface, err error, deprovision bool) error {
	var status corev1.ConditionStatus

	var reason unikornv1.ConditionReason

	var message string

	switch {
	case err == nil:
		status = corev1.ConditionTrue
		reason = unikornv1.ConditionReasonProvisioned
		message = "Provisioned"

		if deprovision {
			reason = unikornv1.ConditionReasonDeprovisioned
			message = "Deprovisioned"
		}
	case errors.Is(err, provisioners.ErrYield):
		status = corev1.ConditionFalse
		reason = unikornv1.ConditionReasonProvisioning
		message = "Provisioning"

		if deprovision {
			reason = unikornv1.ConditionReasonDeprovisioning
			message = "Deprovisioning"
		}
	case errors.Is(err, context.Canceled):
		status = corev1.ConditionFalse
		reason = unikornv1.ConditionReasonCancelled
		message = "Aborted due to controller shutdown"
	default:
		status = corev1.ConditionFalse
		reason = unikornv1.ConditionReasonErrored
		message = fmt.Sprintf("Unhandled error: %v", err)
	}

	object.StatusConditionWrite(unikornv1.ConditionAvailable, status, reason, message)

	if err := r.manager.GetClient().Status().Update(ctx, object); err != nil {
		return err
	}

	return nil
}
