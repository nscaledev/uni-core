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

package manager_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"
	"github.com/unikorn-cloud/core/pkg/manager"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/event"
)

// newTestReconciler builds a non-polling reconciler, the state the predicate
// reads to decide whether it may filter at all.
func newTestReconciler() *manager.Reconciler {
	return manager.NewReconciler(managerOptions(), nil, nil, nil)
}

// newPollingTestReconciler builds a polling one.
func newPollingTestReconciler() *manager.Reconciler {
	return manager.NewReconciler(managerOptionsWithRequeuePeriod(), nil, nil, nil, manager.WithPolling())
}

func newPredicateResource(generation, processed int64, deleting bool) *unikornv1fake.GenerationalResource {
	resource := &unikornv1fake.GenerationalResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  testNamespace,
			Name:       testName,
			Generation: generation,
		},
		Status: unikornv1fake.GenerationalResourceStatus{
			ProcessedGeneration: processed,
		},
	}

	if deleting {
		now := metav1.NewTime(time.Now())
		resource.DeletionTimestamp = &now
	}

	return resource
}

// TestGenerationUnprocessedCreate covers the filter itself: the start up list
// delivers every resource as a create, and only those with work outstanding
// should be let through.
func TestGenerationUnprocessedCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		generation int64
		processed  int64
		deleting   bool
		want       bool
	}{
		{name: "spec moved since last finished", generation: 2, processed: 1, want: true},
		{name: "never processed", generation: 1, processed: 0, want: true},
		{name: "already finished with this generation", generation: 3, processed: 3, want: false},
		// Deleted while the process was down.  Listed on start up as a create
		// with a deletion timestamp already set, and it has a finalizer and
		// provider resources outstanding whatever the generation says.  Dropping
		// this wedges the resource in Terminating and leaks what it owns.
		{name: "deleted while down, generation finished", generation: 3, processed: 3, deleting: true, want: true},
		{name: "deleted while down, generation outstanding", generation: 4, processed: 3, deleting: true, want: true},
	}

	filter := manager.GenerationUnprocessed[*unikornv1fake.GenerationalResource](newTestReconciler())

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := filter.Create(event.TypedCreateEvent[*unikornv1fake.GenerationalResource]{
				Object: newPredicateResource(test.generation, test.processed, test.deleting),
			})

			assert.Equal(t, test.want, got)
		})
	}
}

// TestGenerationUnprocessedPollingKeepsGenerationFilter is the guard that makes
// the worst wiring mistake inert instead of catastrophic.  Polling is self
// sustaining and the start up create event is the only thing that starts the
// chain, so filtering it would leave the controller observing nothing at all for
// the life of the process while reporting healthy.  Refusing new stamps in the
// reconciler cannot cover this on its own: a release that added polling to a
// controller which had previously settled its fleet would find those stamps
// already on disk, and could never reconcile the resources again to clear them.
func TestGenerationUnprocessedPollingKeepsGenerationFilter(t *testing.T) {
	t.Parallel()

	filter := manager.GenerationUnprocessed[*unikornv1fake.GenerationalResource](newPollingTestReconciler())

	settled := newPredicateResource(3, 3, false)

	// Settled, and stamped by some earlier non-polling release: still let
	// through, because a filtered create would stop polling dead.
	assert.True(t, filter.Create(event.TypedCreateEvent[*unikornv1fake.GenerationalResource]{Object: settled}))

	// But only our own arm is disabled.  This function stands in for
	// TypedGenerationChangedPredicate, so passing everything would swap a
	// generation filter for no filter - and a polling controller's own status
	// writes would then wake it, write again, and loop at whatever rate the
	// provisioner runs, ignoring the requeue period.
	assert.False(t, filter.Update(event.TypedUpdateEvent[*unikornv1fake.GenerationalResource]{
		ObjectOld: settled,
		ObjectNew: settled,
	}))
}

// TestGenerationUnprocessedUpdateArm pins the half the predicate composes in
// itself.  Wired without it, the update arm would pass everything, so the
// controller would reconcile at a generation it had already stamped - and a pass
// that then failed terminally would be filtered out of every subsequent start
// up, quietly destroying the "a restart retries ErrTerminal" recovery that
// markProcessed goes out of its way to preserve.
func TestGenerationUnprocessedUpdateArm(t *testing.T) {
	t.Parallel()

	settled := newPredicateResource(3, 3, false)
	outstanding := newPredicateResource(4, 3, false)

	filter := manager.GenerationUnprocessed[*unikornv1fake.GenerationalResource](newTestReconciler())

	// A same-generation write - a status update, say - is dropped.
	assert.False(t, filter.Update(event.TypedUpdateEvent[*unikornv1fake.GenerationalResource]{
		ObjectOld: settled,
		ObjectNew: settled,
	}))

	// A spec edit is not.
	assert.True(t, filter.Update(event.TypedUpdateEvent[*unikornv1fake.GenerationalResource]{
		ObjectOld: settled,
		ObjectNew: outstanding,
	}))
}

// TestGenerationUnprocessedPassesDeletes checks the arms this predicate does not
// touch.  It governs one watch - the controller's own primary watch - and a
// consumer's secondary watches on other resources are separate source.Kind calls
// with their own predicates, which is what separates this from skipping inside
// the reconciler: that sits downstream of every event source and would swallow
// them all.
func TestGenerationUnprocessedPassesDeletes(t *testing.T) {
	t.Parallel()

	// Settled: the create arm would drop this one.
	object := newPredicateResource(3, 3, false)

	filter := manager.GenerationUnprocessed[*unikornv1fake.GenerationalResource](newTestReconciler())

	assert.True(t, filter.Delete(event.TypedDeleteEvent[*unikornv1fake.GenerationalResource]{
		Object: object,
	}))

	assert.True(t, filter.Generic(event.TypedGenericEvent[*unikornv1fake.GenerationalResource]{
		Object: object,
	}))
}
