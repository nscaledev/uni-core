/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.
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
	"errors"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/cd/argocd"
	"github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/provisioninglog"
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

	// If it's being deleted, ignore if there are no finalizers, Kubernetes is in
	// charge now.  If the finalizer is still in place, run the deprovisioning.
	if object.GetDeletionTimestamp() != nil {
		if len(object.GetFinalizers()) == 0 {
			return reconcile.Result{}, nil
		}

		log.Info("deleting object")

		return r.reconcileDelete(ctx, provisioner, object)
	}

	if object.Paused() {
		log.Info("reconcilication paused")

		return reconcile.Result{}, nil
	}

	// Create or update the resource.
	log.Info("reconciling object")

	return r.reconcileNormal(ctx, provisioner, object)
}

// reconcileDelete handles object deletion.
// In the Deleting phase we wait for any references or dependencies to be cleaned.
// In the Draining phase we hand off to the provision to clean up any resources.
// In the Finalizing phase we remove our finalizer to allow deletion.
func (r *Reconciler) reconcileDelete(ctx context.Context, provisioner provisioners.Provisioner, object unikornv1.ManagableResourceInterface) (reconcile.Result, error) {
	log := log.FromContext(ctx)

	references := GetResourceReferences(object)

	var perr error

	switch {
	// Resources from one service can inhibit the deletion of those in others to
	// enforce deletion ordering, for example a cluster can prevent project deletion
	// until it's done cleanup.
	case len(references) > 0:
		log.Info("awaiting resource reference deletion", "references", references)

		perr = provisioners.ErrYield
	// Wait for any owned resources to be cleaned up first.
	case controllerutil.ContainsFinalizer(object, metav1.FinalizerDeleteDependents):
		log.Info("awaiting owned resource deletion")

		perr = provisioners.ErrYield
	default:
		perr = provisioner.Deprovision(ctx)
	}

	// Always update the condition, this may fail if someone has poked the resource
	// e.g, added a finalizer, then just requeue, no need for an error.
	if err := r.handleReconcileCondition(ctx, object, perr, true); err != nil {
		log.Info("failed to update status, enqueuing retry")

		//nolint:nilerr
		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	// If anything went wrong, requeue for another attempt.
	// Errors here actually mean something, a yield means it'll sort itself out
	// via eventual consistency, we expect everything to be handled gracefully.
	if perr != nil {
		if !errors.Is(perr, provisioners.ErrYield) {
			// This will result in an exponential backoff, so you want
			// to avoid it!
			return reconcile.Result{}, perr
		}

		log.Info("controller yielding", "message", perr)

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	// All good, signal the resource can be deleted.
	if ok := controllerutil.RemoveFinalizer(object, constants.Finalizer); ok {
		if err := r.manager.GetClient().Update(ctx, object); err != nil {
			log.Info("failed to remove finalizer", "error", err)

			return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
		}
	}

	log.Info("deletion complete")

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
		//nolint:nilerr
		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	// If anything went wrong, requeue for another attempt.
	// NOTE: DO NOT return an error, and use a constant period or you will
	// suffer from an exponential back-off and kill performance.
	if perr != nil {
		// Terminal dispositions are parked, not retried: requeuing them just
		// burns the workqueue on a failure that will not self-heal (see the
		// provisioners package for the ErrTerminal/ErrUserActionRequired
		// rationale). The condition has already been written above, so the
		// failure remains visible. Recovery is out-of-band: a spec change wakes
		// an ErrUserActionRequired resource via the generation-change watch
		// predicate, while ErrTerminal awaits operator intervention.
		if provisioners.IsTerminal(perr) {
			log.Error(perr, "provisioning terminally failed, parking resource")

			return reconcile.Result{}, nil
		}

		if !errors.Is(perr, provisioners.ErrYield) {
			log.Error(perr, "provisioning failed unexpectedly")
		}

		return reconcile.Result{RequeueAfter: constants.DefaultYieldTimeout}, nil
	}

	log.Info("reconcile complete")

	return reconcile.Result{}, nil
}

// handleReconcileCondition maps the outcome of a (de)provision — the error, or
// nil on success — onto the resource's Available condition. It works in two
// distinct phases, and reads best with that in mind:
//
//  1. Disposition → generic condition. The switch below derives the status and a
//     lifecycle-default reason/message (Provisioned/Provisioning/Errored, or the
//     Deprovision* equivalents) from the error's disposition *alone*. This is
//     everything a bare sentinel error — e.g. a plain provisioners.ErrYield with
//     no detail — can tell us, and it is what keys off the same disposition the
//     requeue logic branches on (see reconcileNormal/reconcileDelete).
//  2. Typed error → data enrichment. A typed *provisioners.Error additionally
//     carries a specific reason and a user-safe message. When present, those
//     fall through and *override* the phase-1 reason/message (status is left as
//     phase 1 set it). So the switch is not the last word: for a typed error it
//     only supplies the fallback that the enrichment block then replaces.
func (r *Reconciler) handleReconcileCondition(ctx context.Context, object unikornv1.ManagableResourceInterface, err error, deprovision bool) error {
	var status corev1.ConditionStatus

	var reason unikornv1.ProvisioningConditionReason

	var message string

	// Capture the prior Available condition so the provisioning log below can be
	// edge-triggered: emit only when the (status, reason, message) tuple actually
	// changes, never on every poll. Nil when there is no condition yet.
	prior, _ := unikornv1.GetAvailableCondition(object)

	// Phase 1: derive status and a lifecycle-default reason/message from the
	// disposition. context.Canceled is the one outcome we deliberately do not
	// record, leaving the existing condition untouched.
	switch {
	case err == nil:
		status = corev1.ConditionTrue
		reason = unikornv1.ConditionReasonProvisioned
		message = "provisioned"

		if deprovision {
			reason = unikornv1.ConditionReasonDeprovisioned
			message = "deprovisioned"
		}
	case errors.Is(err, provisioners.ErrYield):
		status = corev1.ConditionFalse
		reason = unikornv1.ConditionReasonProvisioning
		message = "provisioning"

		if deprovision {
			reason = unikornv1.ConditionReasonDeprovisioning
			message = "deprovisioning"
		}
	case errors.Is(err, context.Canceled):
		// Leave it as it is.
		return nil
	default:
		// Everything else, including the terminal dispositions
		// (ErrTerminal/ErrUserActionRequired), piggybacks on Errored: there is
		// no observable distinction between a retrying and a terminal failure in
		// the surfaced condition (the difference is purely in the requeue
		// decision, see reconcileNormal). The message is deliberately generic and
		// fixed: this condition is user-visible - it is projected onto the API
		// provisioningStatusDetail and emitted on the provisioning log stream - so
		// an untyped error must never be stringified onto it (CWE-209). The typed
		// override below replaces this with the error's user-safe Message() when
		// it carries one; the raw error is left for reconcileNormal to log
		// operator-side, never surfaced here. Messages are lowercase with no
		// trailing punctuation, matching the Go error-string convention the rest
		// of the codebase follows.
		status = corev1.ConditionFalse
		reason = unikornv1.ConditionReasonErrored
		message = "an unexpected error occurred"
	}

	// Phase 2: enrich. A typed provisioning error carries its own closed-vocabulary
	// reason and a user-safe message; write those straight onto the condition,
	// replacing the phase-1 lifecycle default. The operator-only detail stays in
	// the error's fmt.Errorf wrapping, which reconcileNormal logs — errors.As
	// recovers only the safe surface here, so nothing internal leaks to the user
	// (CWE-209).
	//
	// The override is disposition- and path-agnostic on purpose: it enriches the
	// reason/message regardless of the switch arm, but leaves status alone. Typed
	// errors are failure-side — today only the provision path produces them (the
	// Dependency* constructors), so on the deprovision path this is inert. Were a
	// Deprovision to return one, its failure reason would replace the
	// Deprovisioning lifecycle reason; that is acceptable, not a bug: the coarse
	// API projection keys off the deletion timestamp (not the reason), and the
	// requeue decision keys off the disposition, so only the raw condition Reason
	// would show the blocker — arguably the more useful thing to surface.
	var perr *provisioners.Error
	if errors.As(err, &perr) {
		reason = perr.Reason()
		message = perr.Message()
	}

	// Edge: did this transition actually change the surfaced provisioning state?
	changed := prior == nil ||
		prior.Status != status ||
		prior.Reason != reason ||
		prior.Message != message

	object.SetProvisioningCondition(status, reason, message)

	if err := r.manager.GetClient().Status().Update(ctx, object); err != nil {
		return err
	}

	// Emit the provisioning-transition log only once the change is persisted, so
	// the stream reflects committed state (and a failed update simply retries and
	// re-evaluates the edge next reconcile).
	if changed {
		provisioninglog.Emit(ctx, r.manager.GetScheme(), object, provisioninglog.StreamProvisioning, string(status), string(reason), message)
	}

	return nil
}
