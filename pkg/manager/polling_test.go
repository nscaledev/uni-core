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
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
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

	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// otherCondition stands in for status a provisioner writes besides Available.
const otherCondition = "Healthy"

// passTest describes the resource a pass runs against and what it returns.
type passTest struct {
	perr     error
	polling  bool
	deleting bool
	// prior is the Available condition the resource starts with.
	prior *metav1.Condition
	// funcs lets a test make client calls fail.
	funcs interceptor.Funcs
	// options are added to the reconciler's options.
	options []manager.ReconcilerOption
}

func provisioned() *metav1.Condition {
	return &metav1.Condition{
		Type:    string(unikornv1.ConditionAvailable),
		Status:  metav1.ConditionTrue,
		Reason:  string(unikornv1.ConditionReasonProvisioned),
		Message: "provisioned",
	}
}

func errored() *metav1.Condition {
	return &metav1.Condition{
		Type:    string(unikornv1.ConditionAvailable),
		Status:  metav1.ConditionFalse,
		Reason:  string(unikornv1.ConditionReasonErrored),
		Message: "an unexpected error occurred",
	}
}

// newPassTest builds a provisioner whose pass sets otherCondition and then
// returns test.perr.
func newPassTest(t *testing.T, c *gomock.Controller, test passTest) (*testContext, *manager.Reconciler) {
	t.Helper()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  testNamespace,
			Name:       testName,
			Finalizers: []string{constants.Finalizer},
		},
	}

	if test.deleting {
		request.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	}

	if test.prior != nil {
		meta.SetStatusCondition(&request.Status.Conditions, *test.prior)
	}

	tc := mustNewTestContextWithInterceptor(t, test.funcs, request)

	object := &unikornv1fake.ManagedResource{}

	pass := func(_ context.Context) error {
		meta.SetStatusCondition(&object.Status.Conditions, metav1.Condition{
			Type:   otherCondition,
			Status: metav1.ConditionTrue,
			Reason: "Observed",
		})

		return test.perr
	}

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(object)

	if test.deleting {
		p.EXPECT().Deprovision(gomock.Any()).DoAndReturn(pass)
	} else {
		p.EXPECT().Provision(gomock.Any()).DoAndReturn(pass)
	}

	opts := managerOptions()

	var reconcilerOptions []manager.ReconcilerOption

	if test.polling {
		opts = managerOptionsWithRequeuePeriod()

		reconcilerOptions = append(reconcilerOptions, manager.WithPolling())
	}

	reconcilerOptions = append(reconcilerOptions, test.options...)

	reconciler := manager.NewReconciler(opts, nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p }, reconcilerOptions...)

	return tc, reconciler
}

func mustGetResource(t *testing.T, tc *testContext) *unikornv1fake.ManagedResource {
	t.Helper()

	var resource unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(t.Context(), newNamespacedName(testNamespace, testName), &resource))

	return &resource
}

// TestReconcilePollingCadence checks the one cadence rule for a polling
// controller: a plain yield is retried on the yield timeout, because work is in
// flight, and every other outcome is looked at again at the resource's next
// slot, in the low lane.  Nothing parks, because nothing watches the external
// system to wake a parked resource.  The condition still comes from the error.
func TestReconcilePollingCadence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		perr    error
		flat    bool
		status  corev1.ConditionStatus
		reason  unikornv1.ProvisioningConditionReason
		message string
	}{
		{
			name:    "success",
			status:  corev1.ConditionTrue,
			reason:  unikornv1.ConditionReasonProvisioned,
			message: "provisioned",
		},
		{
			name:    "yield",
			perr:    provisioners.ErrYield,
			flat:    true,
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonProvisioning,
			message: "provisioning",
		},
		{
			name:    "typed yield",
			perr:    provisioners.Yield(unikornv1.ConditionReasonDependencyNotReady, "network is not ready"),
			flat:    true,
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonDependencyNotReady,
			message: "network is not ready",
		},
		{
			name:    "recheck",
			perr:    provisioners.Recheck(unikornv1.ConditionReasonErrored, "the server is in error"),
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonErrored,
			message: "the server is in error",
		},
		{
			name:    "bare recheck",
			perr:    provisioners.ErrRecheck,
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonProvisioning,
			message: "provisioning",
		},
		{
			name:    "terminal",
			perr:    provisioners.Terminal(unikornv1.ConditionReasonDependencyNotFound, "gone"),
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonDependencyNotFound,
			message: "gone",
		},
		{
			name:    "user action required",
			perr:    provisioners.UserActionRequired(unikornv1.ConditionReasonErrored, "fix your spec"),
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonErrored,
			message: "fix your spec",
		},
		{
			name:    "unexpected error",
			perr:    errUnhandled,
			status:  corev1.ConditionFalse,
			reason:  unikornv1.ConditionReasonErrored,
			message: "an unexpected error occurred",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			tc, reconciler := newPassTest(t, c, passTest{perr: test.perr, polling: true, prior: provisioned()})

			if test.flat {
				result := reconcileFlat(t.Context(), t, reconciler)
				assert.Nil(t, result.Priority, "a yield keeps the priority of its pass")
			} else {
				result := reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)
				assert.Equal(t, ptr.To(handler.LowPriority), result.Priority)
			}

			resource := mustGetResource(t, tc)
			mustAssertStatus(t, resource, test.status, test.reason)

			condition, err := unikornv1.GetAvailableCondition(resource)
			assert.NoError(t, err)
			assert.Equal(t, test.message, condition.Message)
			assert.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, otherCondition), "the rest of status must be saved")
		})
	}
}

// TestReconcileCreateRecheckWithoutPolling checks a recheck is the yield it
// wraps when there is no poll to wait for: parking would strand it.
func TestReconcileCreateRecheckWithoutPolling(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newPassTest(t, c, passTest{perr: provisioners.Recheck(unikornv1.ConditionReasonErrored, "the server is in error")})

	reconcileFlat(t.Context(), t, reconciler)

	mustAssertStatus(t, mustGetResource(t, tc), corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
}

// TestReconcileDeleteRecheck checks a recheck from Deprovision is the yield it
// wraps: it retries on the yield timeout rather than taking the backoff an
// unexpected error gets, and reports Deprovisioning.
func TestReconcileDeleteRecheck(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newPassTest(t, c, passTest{perr: provisioners.ErrRecheck, polling: true, deleting: true, prior: provisioned()})

	reconcileFlat(t.Context(), t, reconciler)

	resource := mustGetResource(t, tc)
	assert.Contains(t, resource.Finalizers, constants.Finalizer)
	mustAssertStatus(t, resource, corev1.ConditionFalse, unikornv1.ConditionReasonDeprovisioning)
}

// TestReconcilePollingStatusUpdateFailed checks a failed status write takes the
// cadence of the pass's own outcome.  A flat retry would run every polling
// resource every ten seconds while the API server is struggling, and all on the
// same beat when it recovers.
func TestReconcilePollingStatusUpdateFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		perr error
		flat bool
	}{
		{
			name: "success",
		},
		{
			name: "unexpected error",
			perr: errUnhandled,
		},
		{
			name: "yield",
			perr: provisioners.ErrYield,
			flat: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			tc, reconciler := newPassTest(t, c, passTest{perr: test.perr, polling: true, funcs: interceptor.Funcs{SubResourceUpdate: failStatusUpdate}})

			if test.flat {
				reconcileFlat(t.Context(), t, reconciler)
			} else {
				reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)
			}
		})
	}
}

// logSink records the messages a logger was asked to log, by level.
type logSink struct {
	mutex  sync.Mutex
	errors []string
	infos  []string
}

var _ logr.LogSink = &logSink{}

func (s *logSink) Init(logr.RuntimeInfo)          {}
func (s *logSink) Enabled(int) bool               { return true }
func (s *logSink) WithValues(...any) logr.LogSink { return s }
func (s *logSink) WithName(string) logr.LogSink   { return s }

func (s *logSink) Info(_ int, message string, _ ...any) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.infos = append(s.infos, message)
}

func (s *logSink) Error(_ error, message string, _ ...any) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.errors = append(s.errors, message)
}

func (s *logSink) errorMessages() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.errors
}

func (s *logSink) infoMessages() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.infos
}

// TestReconcileFailureLogLevel checks only a failure without a disposition is
// logged at error level, and every other failure but a plain yield at info.
// Info means handled, error means a surprise: a polling controller repeats a
// handled failure every period, and logging that at error level would page
// someone once a minute per resource.
func TestReconcileFailureLogLevel(t *testing.T) {
	t.Parallel()

	const (
		levelNone  = ""
		levelInfo  = "info"
		levelError = "error"
	)

	tests := []struct {
		name  string
		perr  error
		prior *metav1.Condition
		level string
	}{
		{
			name:  "unexpected error",
			perr:  errUnhandled,
			prior: provisioned(),
			level: levelError,
		},
		{
			name:  "repeated unexpected error",
			perr:  errUnhandled,
			prior: errored(),
			level: levelError,
		},
		{
			name:  "terminal",
			perr:  provisioners.Terminal(unikornv1.ConditionReasonErrored, "an unexpected error occurred"),
			prior: provisioned(),
			level: levelInfo,
		},
		{
			name:  "wrapped terminal",
			perr:  fmt.Errorf("%w: quota exceeded", provisioners.ErrTerminal),
			prior: provisioned(),
			level: levelInfo,
		},
		{
			name:  "recheck",
			perr:  provisioners.Recheck(unikornv1.ConditionReasonErrored, "the server is in error"),
			prior: provisioned(),
			level: levelInfo,
		},
		{
			name:  "yield",
			perr:  provisioners.ErrYield,
			prior: provisioned(),
			level: levelNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			_, reconciler := newPassTest(t, c, passTest{perr: test.perr, polling: true, prior: test.prior})

			sink := &logSink{}
			ctx := log.IntoContext(t.Context(), logr.New(sink))

			_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
			assert.NoError(t, err)

			if test.level == levelError {
				assert.Len(t, sink.errorMessages(), 1)
			} else {
				assert.Empty(t, sink.errorMessages())
			}

			if test.level == levelInfo {
				assert.Contains(t, sink.infoMessages(), "provisioning failed")
			} else {
				assert.NotContains(t, sink.infoMessages(), "provisioning failed")
			}
		})
	}
}
