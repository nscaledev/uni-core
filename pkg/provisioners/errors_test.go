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

package provisioners_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	coreconstants "github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// errBare is an untyped error used to exercise the non-*Error fallback paths.
var errBare = errors.New("bare")

// TestDispositions checks each constructor unwraps to the right sentinel and
// that IsTerminal (including through wrapping) classifies them correctly.
func TestDispositions(t *testing.T) {
	t.Parallel()

	r := unikornv1.ConditionReasonErrored

	assert.ErrorIs(t, provisioners.Yield(r, "m"), provisioners.ErrYield)
	assert.False(t, provisioners.IsTerminal(provisioners.Yield(r, "m")))

	assert.True(t, provisioners.IsTerminal(provisioners.Terminal(r, "m")))
	assert.True(t, provisioners.IsTerminal(provisioners.UserActionRequired(r, "m")))
	assert.True(t, provisioners.IsTerminal(fmt.Errorf("wrap: %w", provisioners.Terminal(r, "m"))))

	assert.False(t, provisioners.IsTerminal(provisioners.ErrYield))
	assert.False(t, provisioners.IsTerminal(errBare))
}

// TestReasonAndMessage checks the typed accessors return the reason and user-safe
// message unchanged, and that both survive being recovered from an outer wrapping
// via errors.As while the unsafe wrapping prose stays out of the user surface.
func TestReasonAndMessage(t *testing.T) {
	t.Parallel()

	const (
		reason  = unikornv1.ConditionReasonDependencyNotFound
		message = "Network \"prod-net\" (a1b2) does not exist"
	)

	e := provisioners.Terminal(reason, message)

	assert.Equal(t, reason, e.Reason())
	assert.Equal(t, message, e.Message())

	// Wrapped with operator-only prose: errors.As recovers the typed error, so the
	// safe reason/message are intact and the wrapping never reaches the accessors.
	wrapped := fmt.Errorf("scheduler 10.0.0.1 exploded: %w", e)

	var perr *provisioners.Error

	assert.ErrorAs(t, wrapped, &perr)
	assert.Equal(t, reason, perr.Reason())
	assert.Equal(t, message, perr.Message())
	assert.NotContains(t, perr.Message(), "scheduler 10.0.0.1")

	// The full Error() string still carries everything for logs.
	assert.Contains(t, wrapped.Error(), "scheduler 10.0.0.1")
	assert.Contains(t, wrapped.Error(), string(reason))
}

// TestDependencyConstructors checks the enforced dependency errors: the correct
// disposition and reason per constructor and a user-safe "Kind \"name\" (id)
// <state>" message. describeResource is exercised through these, as it is
// unexported.
func TestDependencyConstructors(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	assert.NoError(t, corev1.AddToScheme(scheme))

	dep := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "a1b2c3",
			Labels: map[string]string{coreconstants.NameLabel: "prod-net"},
		},
	}

	notReady := provisioners.DependencyNotReady(scheme, dep)
	assert.ErrorIs(t, notReady, provisioners.ErrYield)
	assert.False(t, provisioners.IsTerminal(notReady))
	assert.Equal(t, unikornv1.ConditionReasonDependencyNotReady, notReady.Reason())
	assert.Equal(t, `ConfigMap "prod-net" (a1b2c3) is not ready`, notReady.Message())

	failed := provisioners.DependencyFailed(scheme, dep)
	assert.ErrorIs(t, failed, provisioners.ErrYield)
	assert.Equal(t, unikornv1.ConditionReasonDependencyFailed, failed.Reason())
	assert.Equal(t, `ConfigMap "prod-net" (a1b2c3) has failed`, failed.Message())

	// NotFound is terminal; a name-less object (never fetched, or gone) renders
	// the id only.
	gone := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "a1b2c3"}}
	notFound := provisioners.DependencyNotFound(scheme, gone)
	assert.True(t, provisioners.IsTerminal(notFound))
	assert.Equal(t, unikornv1.ConditionReasonDependencyNotFound, notFound.Reason())
	assert.Equal(t, "ConfigMap a1b2c3 does not exist", notFound.Message())
}
