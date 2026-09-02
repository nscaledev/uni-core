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

package manager

import (
	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// GenerationProcessorObject is a managed resource that records the last spec
// generation its controller finished with.
type GenerationProcessorObject interface {
	client.Object
	unikornv1.GenerationProcessor
}

// GenerationUnprocessed drops the create event for a resource whose spec
// generation the controller has already finished with.
//
// WHY: on start up the informer lists every resource and delivers it as a create
// event, so the whole fleet is reconciled from scratch.  For a large fleet that
// is a burst of provider and identity traffic that achieves nothing - the work
// was already done - and it is how a restart turns into a memory spike and a
// crash loop.  Filtering here rather than inside the reconciler matters at that
// scale: a dropped event is never queued, and never costs a provisioner, a
// driver and a client read before being thrown away.
//
// This is a WATCH predicate, and that is the whole point of it.  It governs one
// watch - the controller's own primary watch on its own resource - and cannot
// see any other.  Secondary watches a consumer registers for other resources are
// separate source.Kind calls with their own predicates and are untouched.  That
// is the difference between this and skipping inside the reconciler, which sits
// downstream of every event source, cannot tell them apart, and would swallow
// them all.
//
// On this watch, the create arm compares the stamp and the update arm is the
// ordinary generation comparison - so a same-generation update, such as a status
// write or an annotation edit, is dropped exactly as TypedGenerationChangedPredicate
// has always dropped it.  Do not wire this onto a controller that needs to wake
// on a metadata-only change to its own resource.
//
// WHEN NOT TO USE IT: only wire this in where a restart genuinely has nothing to
// catch up on.  A controller that watches another resource - a compute instance
// tracking a region server, say - misses every change to it while the process is
// down, and its create event on start up is the only chance it gets to notice.
// Dropping that leaves it permanently behind.  The same goes for a polling
// controller, whose whole premise is that the world moves without telling it.
//
// It REPLACES TypedGenerationChangedPredicate rather than sitting beside it, and
// composes the two itself.  Both arms are the same question - has this
// generation got work outstanding - asked of the only evidence each event
// carries: a create compares the stamp, an update compares the two generations.
// Leaving that composition to the caller was a footgun.  On its own the update
// arm passes everything, so the controller reconciles at a generation it has
// already stamped, and a pass that then fails terminally is filtered out of
// every subsequent start up - quietly destroying the "a restart retries
// ErrTerminal" recovery that markProcessed goes out of its way to preserve.
//
//	controller.Watch(source.Kind(
//	    manager.GetCache(),
//	    &unikornv1.Organization{},
//	    &handler.TypedEnqueueRequestForObject[*unikornv1.Organization]{},
//	    coremanager.GenerationUnprocessed[*unikornv1.Organization](f.reconciler),
//	))
//
// The factory has to keep the reconciler from its own Reconciler() method to
// pass it here.  Run calls Reconciler() before RegisterWatches(), so it is
// available; it is not handed to RegisterWatches directly because that would
// change ControllerFactory for every consumer.
//
// It takes the reconciler because it reads a field the reconciler writes, and
// because that is what lets it disable itself.  A polling controller filters
// nothing: polling is self sustaining, each finished pass scheduling the next
// through RequeueAfter, and the start up create event is the only thing that
// begins the chain - filter that and the controller silently observes nothing
// for the life of the process while reporting healthy.  Refusing new stamps in
// the reconciler is not enough on its own, because it cannot clear stamps a
// previous non-polling release already wrote to disk, and those resources can
// never be reconciled again to have them cleared.  Deciding here covers both.
func GenerationUnprocessed[T GenerationProcessorObject](r *Reconciler) predicate.TypedPredicate[T] {
	// Disable only our own arm, not the one we replaced.  Returning a
	// pass-everything predicate here would be worse than doing nothing: this
	// function stands in for TypedGenerationChangedPredicate, so a consumer
	// migrating a polling controller would be swapping a generation filter for no
	// filter, and a polling controller's status tracks something that moves - so
	// its own status writes would wake it, write again, and loop at whatever rate
	// the provisioner runs, ignoring the requeue period entirely.
	if r.requeuePeriod > 0 {
		return &predicate.TypedGenerationChangedPredicate[T]{}
	}

	unprocessed := predicate.TypedFuncs[T]{
		CreateFunc: func(e event.TypedCreateEvent[T]) bool {
			// A resource deleted while this process was down is listed on start
			// up as a create, with its deletion timestamp already set.  It has
			// outstanding work by definition - the finalizer is still on it and
			// the provider resources are still there - whatever the generation
			// says.
			//
			// The API server does bump the generation when it sets a deletion
			// timestamp (apiserver store.go markAsDeleting, and delete.go for the
			// graceful path), so in practice the check below would let it
			// through anyway.  This does not lean on that: both bumps are
			// conditional, they are an implementation detail of a component we do
			// not own, and the failure if either ever stopped holding is a
			// resource wedged in Terminating with its provider resources leaked
			// and nothing logged.
			if !e.Object.GetDeletionTimestamp().IsZero() {
				return true
			}

			return e.Object.ProcessedGeneration() != e.Object.GetGeneration()
		},
	}

	return predicate.And(
		predicate.TypedPredicate[T](&predicate.TypedGenerationChangedPredicate[T]{}),
		predicate.TypedPredicate[T](unprocessed),
	)
}
