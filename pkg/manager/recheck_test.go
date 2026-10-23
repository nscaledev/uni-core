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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	mockprovisioners "github.com/unikorn-cloud/core/pkg/provisioners/mock"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// recheckReason is any reason a typed recheck might carry.
const recheckReason = unikornv1.ConditionReasonErrored

// otherCondition stands in for status a provisioner writes besides Available.
const otherCondition = "Healthy"

// newRecheckTest builds a provisioner whose pass sets otherCondition and then
// returns perr, against a resource that was last Provisioned.
func newRecheckTest(t *testing.T, c *gomock.Controller, perr error, polling bool, deleting bool) (*testContext, *manager.Reconciler) {
	t.Helper()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
	}

	if deleting {
		request.Finalizers = []string{constants.Finalizer}
		request.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	}

	request.SetProvisioningCondition(corev1.ConditionTrue, unikornv1.ConditionReasonProvisioned, "provisioned")

	tc := mustNewTestContext(t, request)

	object := &unikornv1fake.ManagedResource{}

	pass := func(_ context.Context) error {
		meta.SetStatusCondition(&object.Status.Conditions, metav1.Condition{
			Type:   otherCondition,
			Status: metav1.ConditionTrue,
			Reason: "Observed",
		})

		return perr
	}

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(object)

	if deleting {
		p.EXPECT().Deprovision(gomock.Any()).DoAndReturn(pass)
	} else {
		p.EXPECT().Provision(gomock.Any()).DoAndReturn(pass)
	}

	opts := managerOptions()

	var reconcilerOptions []manager.ReconcilerOption

	if polling {
		opts = managerOptionsWithRequeuePeriod()

		reconcilerOptions = append(reconcilerOptions, manager.WithPolling())
	}

	reconciler := manager.NewReconciler(opts, nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p }, reconcilerOptions...)

	return tc, reconciler
}

func mustGetResource(t *testing.T, tc *testContext) *unikornv1fake.ManagedResource {
	t.Helper()

	var resource unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(t.Context(), newNamespacedName(testNamespace, testName), &resource))

	return &resource
}

// TestReconcileCreatePollingRecheck checks a typed recheck is polled like a
// success, at its slot and in the low lane, and reports its reason.  A recheck
// on the yield timeout would hammer a provider that has to move on its own, and
// one that parked would never see it move.
func TestReconcileCreatePollingRecheck(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newRecheckTest(t, c, provisioners.Recheck(recheckReason, "the server is in error"), true, false)

	result := reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)
	assert.Equal(t, ptr.To(handler.LowPriority), result.Priority)

	resource := mustGetResource(t, tc)
	mustAssertStatus(t, resource, corev1.ConditionFalse, recheckReason)

	condition, err := unikornv1.GetAvailableCondition(resource)
	assert.NoError(t, err)
	assert.Equal(t, "the server is in error", condition.Message)
}

// TestReconcileCreatePollingRecheckBare checks a bare recheck leaves the
// Available condition as it was but still saves the rest of status.  It is how
// a pass that could not observe anything says so: an unreachable provider must
// not flip every resource to an error, and must not lose what the pass did see.
func TestReconcileCreatePollingRecheckBare(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	perr := fmt.Errorf("%w: provider unreachable", provisioners.ErrRecheck)

	tc, reconciler := newRecheckTest(t, c, perr, true, false)

	result := reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)
	assert.Equal(t, ptr.To(handler.LowPriority), result.Priority)

	resource := mustGetResource(t, tc)
	mustAssertStatus(t, resource, corev1.ConditionTrue, unikornv1.ConditionReasonProvisioned)
	assert.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, otherCondition), "the rest of status must still be saved")
}

// TestReconcileCreateRecheckWithoutPolling checks a recheck is the yield it
// wraps when there is no poll to wait for: parking would strand it.
func TestReconcileCreateRecheckWithoutPolling(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newRecheckTest(t, c, provisioners.Recheck(recheckReason, "the server is in error"), false, false)

	reconcileFlat(t.Context(), t, reconciler)

	mustAssertStatus(t, mustGetResource(t, tc), corev1.ConditionFalse, recheckReason)
}

// TestReconcileCreateBareRecheckWithoutPolling checks a bare recheck leaves the
// condition alone whether or not the controller polls: only the requeue
// depends on polling, not what "could not observe anything new" means.
func TestReconcileCreateBareRecheckWithoutPolling(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newRecheckTest(t, c, provisioners.ErrRecheck, false, false)

	reconcileFlat(t.Context(), t, reconciler)

	resource := mustGetResource(t, tc)
	mustAssertStatus(t, resource, corev1.ConditionTrue, unikornv1.ConditionReasonProvisioned)
	assert.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, otherCondition), "the rest of status must still be saved")
}

// TestReconcileDeleteRecheck checks a bare recheck from Deprovision is the
// yield it wraps: it retries on the yield timeout rather than taking the
// backoff an unexpected error gets, and reports Deprovisioning rather than
// leaving a stale Provisioned that would tell dependents a dying resource is
// ready.
func TestReconcileDeleteRecheck(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newRecheckTest(t, c, provisioners.ErrRecheck, true, true)

	reconcileFlat(t.Context(), t, reconciler)

	resource := mustGetResource(t, tc)
	assert.Contains(t, resource.Finalizers, constants.Finalizer)
	mustAssertStatus(t, resource, corev1.ConditionFalse, unikornv1.ConditionReasonDeprovisioning)
}
