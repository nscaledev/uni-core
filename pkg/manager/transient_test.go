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
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var errProvider = errors.New("provider unreachable")

// isProviderError stands in for the classifier a consumer passes for the
// errors of the external system it owns.
func isProviderError(err error) bool {
	return errors.Is(err, errProvider)
}

// newTransientTest is newPassTest for a polling controller that classifies
// transient errors with WithTransient(classifiers...).
func newTransientTest(t *testing.T, c *gomock.Controller, perr error, deleting bool, classifiers ...func(error) bool) (*testContext, *manager.Reconciler) {
	t.Helper()

	return newPassTest(t, c, passTest{
		perr:     perr,
		polling:  true,
		deleting: deleting,
		prior:    provisioned(),
		options:  []manager.ReconcilerOption{manager.WithTransient(classifiers...)},
	})
}

// TestReconcileTransient checks a pass that could not reach a service leaves
// the Available condition as it was, saves the rest of status, logs no error,
// and waits for the next slot.  "We could not look" is not news about the
// resource: without this an outage flips every resource to Errored and back.
// Kubernetes errors are core's to classify; anything else is the consumer's.
func TestReconcileTransient(t *testing.T) {
	t.Parallel()

	resource := schema.GroupResource{Group: "unikorn-cloud.org", Resource: "things"}

	tests := []struct {
		name string
		perr error
	}{
		{
			name: "server timeout",
			perr: kerrors.NewServerTimeout(resource, "get", 1),
		},
		{
			name: "timeout",
			perr: kerrors.NewTimeoutError("slow", 1),
		},
		{
			name: "too many requests",
			perr: kerrors.NewTooManyRequests("busy", 1),
		},
		{
			name: "service unavailable",
			perr: kerrors.NewServiceUnavailable("down"),
		},
		{
			name: "network",
			perr: fmt.Errorf("listing: %w", &url.Error{Op: "Get", URL: "https://api", Err: &net.OpError{Op: "dial", Err: errUnhandled}}),
		},
		{
			name: "consumer classified",
			perr: fmt.Errorf("creating server: %w", errProvider),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			tc, reconciler := newTransientTest(t, c, test.perr, false, isProviderError)

			sink := &logSink{}
			ctx := log.IntoContext(t.Context(), logr.New(sink))

			reconcileSlotted(ctx, t, tc, reconciler, testRequeuePeriod)

			resource := mustGetResource(t, tc)
			mustAssertStatus(t, resource, corev1.ConditionTrue, unikornv1.ConditionReasonProvisioned)
			assert.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, otherCondition), "the rest of status must be saved")
			assert.Empty(t, sink.errorMessages())
		})
	}
}

// TestReconcileNotTransient checks what the classifier must not catch: an
// error it does not know, a Kubernetes error that is an answer rather than an
// outage, and any error from a controller that did not ask for classification.
func TestReconcileNotTransient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		perr        error
		classifiers []func(error) bool
		enabled     bool
	}{
		{
			name:    "unknown error",
			perr:    errUnhandled,
			enabled: true,
		},
		{
			name:    "conflict",
			perr:    kerrors.NewConflict(schema.GroupResource{}, "thing", errUnhandled),
			enabled: true,
		},
		{
			name:    "internal error",
			perr:    kerrors.NewInternalError(errUnhandled),
			enabled: true,
		},
		{
			name: "not enabled",
			perr: kerrors.NewServiceUnavailable("down"),
		},
		{
			name:    "wrapped disposition",
			perr:    fmt.Errorf("%w: %w", provisioners.ErrTerminal, errProvider),
			enabled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			var (
				tc         *testContext
				reconciler *manager.Reconciler
			)

			if test.enabled {
				tc, reconciler = newTransientTest(t, c, test.perr, false, append(test.classifiers, isProviderError)...)
			} else {
				tc, reconciler = newPassTest(t, c, passTest{perr: test.perr, polling: true, prior: provisioned()})
			}

			reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)

			mustAssertStatus(t, mustGetResource(t, tc), corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
		})
	}
}

// TestReconcileTypedNotTransient checks a typed error keeps its own reason even
// when it also wraps a transient one: the provisioner decided what to report.
func TestReconcileTypedNotTransient(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	perr := fmt.Errorf("%w: %w", provisioners.Terminal(unikornv1.ConditionReasonErrored, "the server is in error"), errProvider)

	tc, reconciler := newTransientTest(t, c, perr, false, isProviderError)

	reconcileSlotted(t.Context(), t, tc, reconciler, testRequeuePeriod)

	mustAssertStatus(t, mustGetResource(t, tc), corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
}

// TestReconcileDeleteTransient checks the delete path ignores the classifier.
// Left as Provisioned, a dying resource would read as ready to anything gated
// on it.
func TestReconcileDeleteTransient(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc, reconciler := newTransientTest(t, c, kerrors.NewServiceUnavailable("down"), true, isProviderError)

	_, err := reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))
	assert.Error(t, err)

	mustAssertStatus(t, mustGetResource(t, tc), corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
}

// TestReconcileTransientWithoutPolling checks a controller that does not poll
// can use the classifier too, and that a failure with a disposition is never
// hidden by it.  A terminal failure parks such a controller, so leaving the old
// Provisioned in place would read as ready for ever.
func TestReconcileTransientWithoutPolling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		perr   error
		status corev1.ConditionStatus
		reason unikornv1.ProvisioningConditionReason
	}{
		{
			name:   "unreachable",
			perr:   errProvider,
			status: corev1.ConditionTrue,
			reason: unikornv1.ConditionReasonProvisioned,
		},
		{
			name:   "wrapped terminal",
			perr:   fmt.Errorf("%w: %w", provisioners.ErrTerminal, errProvider),
			status: corev1.ConditionFalse,
			reason: unikornv1.ConditionReasonErrored,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			tc, reconciler := newPassTest(t, c, passTest{
				perr:    test.perr,
				prior:   provisioned(),
				options: []manager.ReconcilerOption{manager.WithTransient(isProviderError)},
			})

			_, err := reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))
			assert.NoError(t, err)

			mustAssertStatus(t, mustGetResource(t, tc), test.status, test.reason)
		})
	}
}

// TestReconcileCancelledNotTransient checks a client call cut off by shutdown
// is a cancelled pass, not an outage, though the client wraps it in a network
// error.  A cancelled pass writes nothing.
func TestReconcileCancelledNotTransient(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	perr := &url.Error{Op: "Get", URL: "https://api", Err: context.Canceled}

	tc, reconciler := newTransientTest(t, c, perr, false)

	_, err := reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))
	assert.NoError(t, err)

	assert.False(t, meta.IsStatusConditionTrue(mustGetResource(t, tc).Status.Conditions, otherCondition), "a cancelled pass must not write status")
}

// TestUnreachable checks the exported test is the one WithTransient applies, so
// a provisioner that keeps its own status in step with the condition agrees
// with it on every case.
func TestUnreachable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		unreachable bool
	}{
		{name: "nil"},
		{name: "unknown", err: errUnhandled},
		{name: "kubernetes", err: kerrors.NewServiceUnavailable("down"), unreachable: true},
		{name: "classified", err: fmt.Errorf("creating server: %w", errProvider), unreachable: true},
		{name: "disposition", err: fmt.Errorf("%w: %w", provisioners.ErrTerminal, errProvider)},
		{name: "cancelled", err: &url.Error{Op: "Get", URL: "https://api", Err: context.Canceled}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.unreachable, manager.Unreachable(test.err, isProviderError))
		})
	}
}
