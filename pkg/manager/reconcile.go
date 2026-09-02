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
	"math"
	"math/rand/v2"
	"time"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/cd/argocd"
	"github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	"github.com/unikorn-cloud/core/pkg/provisioners/application"
	"github.com/unikorn-cloud/core/pkg/provisioninglog"

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

// requeueJitterFraction is the reciprocal of the maximum jitter added to the
// requeue period, i.e. 10 gives up to a tenth of a period of spread.  Enough to
// break a convoy up quickly, small enough that the period still means what an
// operator set it to.
const requeueJitterFraction = 10

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

	// requeuePeriod is how long a successful reconcile waits before revisiting
	// the resource.  Zero - the default - means park and wait for a watch event.
	// Set, and guaranteed positive, by WithPolling.
	//
	// This is resolved once at construction rather than read from options at use
	// site so that the fallback below is applied and logged exactly once, not on
	// every pass.  Run parses flags before building the reconciler, so there is
	// nothing later to observe.
	requeuePeriod time.Duration
}

// ReconcilerOption allows optional reconciler behaviour to be enabled without
// breaking existing callers of NewReconciler.
type ReconcilerOption func(*Reconciler)

// WithPolling makes a successful reconcile requeue after the controller's
// configured requeue period instead of parking until the next watch event.
//
// WHY: a controller whose resources are backed by an external system that moves
// on its own - a cloud provider, say - cannot learn that the world has drifted
// from a watch on its own CRDs, because nothing writes to them when the drift
// happens.  Historically that gap was filled by a second process polling the
// provider and patching status.  Two processes writing one resource's status is
// a split brain: neither owns the conditions, transitions authored by one are
// invisible to the other, and the writes churn or conflict.  Polling in the
// reconcile pass instead means one writer, one read, one derivation.
//
// This is deliberately not a flag.  Whether a controller needs to poll is a
// property of what it manages, not something an operator should be able to turn
// off; only the period is tunable.  Configuration cannot defeat it either: a
// non-positive period falls back to constants.DefaultRequeuePeriod rather than
// silently parking, because a polling controller that never polls goes stale
// while still reporting success.
//
// A polling controller must be careful with terminal dispositions, because
// parking still means parking: there is no watch on the external system to
// revive it, and ErrTerminal has no generation-bump wake either.  Never return
// Terminal() for external-system state that can recover on its own - a provider
// catalogue that blips during an update, say - or the resource stays parked long
// after the fault has cleared.  Yield() is the disposition for anything the next
// poll might find fixed.
func WithPolling() ReconcilerOption {
	return func(r *Reconciler) {
		// NewReconciler populates the struct before applying options, so the
		// options are readable here.
		r.requeuePeriod = r.options.RequeuePeriod

		// A polling controller with no period never polls, and does so silently:
		// controller-runtime treats a non-positive RequeueAfter as "done", so the
		// resource goes stale while continuing to report success.  Polling is a
		// code-level decision about what backs the resource, so configuration must
		// not be able to defeat it - and a zero period is reachable without anyone
		// typing it, because Options can be built directly rather than through
		// AddFlags.
		if r.requeuePeriod <= 0 {
			log.Log.WithName("manager").Info("requeue period is not positive, falling back to the default",
				"configured", r.options.RequeuePeriod, "period", constants.DefaultRequeuePeriod)

			r.requeuePeriod = constants.DefaultRequeuePeriod
		}
	}
}

// NewReconciler creates a new reconciler.
func NewReconciler(options *options.Options, controllerOptions ControllerOptions, manager manager.Manager, createProvisioner ProvisionerCreateFunc, reconcilerOptions ...ReconcilerOption) *Reconciler {
	r := &Reconciler{
		options:           options,
		manager:           manager,
		createProvisioner: createProvisioner,
		controllerOptions: controllerOptions,
	}

	for _, o := range reconcilerOptions {
		o(r)
	}

	return r
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

// markProcessed records the current generation as finished with, so that
// GenerationUnprocessed can drop the create event for it on the next start up.
// Resources that do not implement GenerationProcessor are left alone.
//
// ONLY a success stamps.  Every failure is left outstanding, however terminal it
// looks, and that asymmetry is deliberate rather than lazy.
//
// A yield or an unexpected error has scheduled another pass, so the generation
// is plainly not finished with.  The terminal dispositions are the interesting
// ones: both park, so nothing retries them in process, and stamping either would
// mean the resource is also filtered out of every future start up.  That turns
// "parked until something changes" into "parked forever", and it does so
// precisely when the controller's own judgement was wrong - a provisioner bug
// that misclassifies valid specs would stamp the whole affected fleet, and
// fixing the bug would not re-drive any of it.  Leaving them unstamped costs one
// wasted pass per parked resource per restart, which is a trivial price for
// keeping a redeploy as a recovery route.
//
// Stickiness falls out of the same rule for free: the failure arms never touch
// the field, so a transient error cannot drag a settled resource back onto the
// expensive path.
//
// A deprovision never stamps.  The resource is on its way out, nothing would
// read the field, and a half-deleted resource that reads as settled is a worse
// thing to leave behind than one that reconciles once more.
func (r *Reconciler) markProcessed(object unikornv1.ManagableResourceInterface, err error, deprovision bool) {
	if deprovision {
		return
	}

	// A polling controller never stamps, which is what makes wiring
	// GenerationUnprocessed onto one harmless rather than catastrophic.
	//
	// Polling is self sustaining: each finished pass schedules the next through
	// RequeueAfter, and the informer's start up create event is the only thing
	// that starts that chain.  Filter that event and reconcileNormal never runs,
	// so nothing is ever requeued, so the controller silently observes nothing
	// for the life of the process while reporting perfectly healthy.
	//
	// Leaving the generation permanently unstamped means the predicate's
	// comparison never matches and it filters nothing, so the two compose to a
	// no-op instead.  Guarding here rather than in the predicate is deliberate:
	// the predicate is wired by the consumer and cannot see how the reconciler
	// was built, but the reconciler owns the data the predicate reads.
	if r.requeuePeriod > 0 {
		return
	}

	if err != nil {
		return
	}

	processor, ok := object.(unikornv1.GenerationProcessor)
	if !ok {
		return
	}

	processor.SetProcessedGeneration(object.GetGeneration())
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
		return reconcile.Result{RequeueAfter: jittered(constants.DefaultYieldTimeout)}, nil
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

		return reconcile.Result{RequeueAfter: jittered(constants.DefaultYieldTimeout)}, nil
	}

	// All good, signal the resource can be deleted.
	if ok := controllerutil.RemoveFinalizer(object, constants.Finalizer); ok {
		if err := r.manager.GetClient().Update(ctx, object); err != nil {
			log.Info("failed to remove finalizer", "error", err)

			return reconcile.Result{RequeueAfter: jittered(constants.DefaultYieldTimeout)}, nil
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
		return reconcile.Result{RequeueAfter: jittered(constants.DefaultYieldTimeout)}, nil
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

		return reconcile.Result{RequeueAfter: jittered(constants.DefaultYieldTimeout)}, nil
	}

	log.Info("reconcile complete")

	// requeuePeriod is zero unless WithPolling was given, and controller-runtime
	// treats a zero RequeueAfter as "done", so this is the unchanged park for
	// everything that has not opted in: the resource is level-triggered and a
	// watch wakes it when the spec changes.  A polling controller instead
	// revisits on a timer, because the external system backing it moves without
	// writing to the resource and so generates no watch event.
	//
	// Note this deliberately does NOT apply to the terminal branch above.  A
	// terminal disposition means the failure will not self-heal, so requeuing it
	// is precisely the workqueue burn ErrTerminal exists to stop.
	return reconcile.Result{RequeueAfter: jittered(r.requeuePeriod)}, nil
}

// jittered spreads a requeue duration so that resources which requeue together
// do not stay together.
//
// WHY: controller-runtime schedules a requeue for exactly the duration given, so
// resources that finish together re-arm together.  The initial informer list
// drains the whole fleet into a narrow window, and every resource then wakes at
// exactly one interval after its own completion, so that window is not
// transient - it is preserved for the life of the process, and the fleet hits
// the provider and the API server in one burst per interval with both idle in
// between.
//
// Every fixed requeue in this package goes through here, not just the poll
// period.  A yield is the more common requeue on exactly the controllers polling
// targets, and it is six times more frequent, so leaving it fixed would let the
// convoy re-form on the yield path as fast as the poll path spread it.
//
// The jitter is added, never subtracted, so the duration is a floor: a caller
// asking for a minute is never woken more often than once a minute.  Being a
// random walk it decorrelates over tens of passes rather than immediately - the
// first wake after start up is still fairly bunched - which is the price of
// keeping the configured period meaning what it says.
func jittered(d time.Duration) time.Duration {
	// Covers the park case (zero) and any duration too small to divide, which
	// matters because rand.N panics on a non-positive argument.
	jitter := d / requeueJitterFraction
	if jitter <= 0 {
		return d
	}

	// A duration near the end of the int64 range would wrap to a negative, and
	// controller-runtime reads a non-positive RequeueAfter as "done" - so an
	// absurd period would silently stop the polling it configured.
	if d > math.MaxInt64-jitter {
		return d
	}

	// This picks a wake-up time, not a secret.  A predictable jitter is fine -
	// the only thing it has to defeat is the fleet's own convoy, not an
	// adversary - so the cost of a CSPRNG buys nothing here.
	//nolint:gosec
	return d + rand.N(jitter)
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
//
//nolint:cyclop
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

	// Stamped here, not by the caller, so it lands in the same status write as
	// the condition.  Written separately the two can tear: a condition saying
	// provisioned with a generation still reading outstanding costs a redundant
	// reconcile, and the reverse - settled generation, no condition - is a
	// resource that reads as done having never reported it.
	r.markProcessed(object, err, deprovision)

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
