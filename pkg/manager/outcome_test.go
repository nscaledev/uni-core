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
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/manager"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

// outcomes returns the reconcile outcome counter's series as attribute sets
// mapped to their counts.
func outcomes(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Distinct]int64 {
	t.Helper()

	var data metricdata.ResourceMetrics

	require.NoError(t, reader.Collect(t.Context(), &data))

	result := map[attribute.Distinct]int64{}

	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "reconcile_outcomes" {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			assert.True(t, sum.IsMonotonic)

			for _, point := range sum.DataPoints {
				result[point.Attributes.Equivalent()] = point.Value
			}
		}
	}

	return result
}

func outcomeSet(operation, outcome string, reason unikornv1.ProvisioningConditionReason) attribute.Distinct {
	set := attribute.NewSet(
		attribute.String("operation", operation),
		attribute.String("outcome", outcome),
		attribute.String("reason", string(reason)),
	)

	return set.Equivalent()
}

// TestReconcileOutcomeCounted checks every pass is counted by what it
// returned.  The condition cannot show an outage, because an unreached service
// leaves it alone, and an untyped failure and a customer's mistake can share a
// reason; the outcome label is what tells them apart.
func TestReconcileOutcomeCounted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		perr      error
		deleting  bool
		operation string
		outcome   string
		reason    unikornv1.ProvisioningConditionReason
	}{
		{
			name:      "success",
			operation: "provision",
			outcome:   "success",
			reason:    unikornv1.ConditionReasonProvisioned,
		},
		{
			name:      "yield",
			perr:      provisioners.Yield(unikornv1.ConditionReasonDependencyNotReady, "network is not ready"),
			operation: "provision",
			outcome:   "yield",
			reason:    unikornv1.ConditionReasonDependencyNotReady,
		},
		{
			name:      "recheck",
			perr:      provisioners.Recheck(unikornv1.ConditionReasonErrored, "the server is in error"),
			operation: "provision",
			outcome:   "recheck",
			reason:    unikornv1.ConditionReasonErrored,
		},
		{
			name:      "terminal",
			perr:      provisioners.Terminal(unikornv1.ConditionReasonErrored, "gone"),
			operation: "provision",
			outcome:   "terminal",
			reason:    unikornv1.ConditionReasonErrored,
		},
		{
			name:      "user action required",
			perr:      provisioners.UserActionRequired(unikornv1.ConditionReasonErrored, "fix your spec"),
			operation: "provision",
			outcome:   "user_action_required",
			reason:    unikornv1.ConditionReasonErrored,
		},
		{
			name:      "unexpected error",
			perr:      errUnhandled,
			operation: "provision",
			outcome:   "error",
			reason:    unikornv1.ConditionReasonErrored,
		},
		{
			name:      "transient",
			perr:      kerrors.NewServiceUnavailable("down"),
			operation: "provision",
			outcome:   "transient",
		},
		{
			name:      "deprovision",
			deleting:  true,
			operation: "deprovision",
			outcome:   "success",
			reason:    unikornv1.ConditionReasonDeprovisioned,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := gomock.NewController(t)
			defer c.Finish()

			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

			_, reconciler := newPassTest(t, c, passTest{
				perr:     test.perr,
				polling:  true,
				deleting: test.deleting,
				prior:    provisioned(),
				options:  []manager.ReconcilerOption{manager.WithTransient(), manager.WithMeterProvider(provider)},
			})

			_, _ = reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))

			assert.Equal(t, map[attribute.Distinct]int64{outcomeSet(test.operation, test.outcome, test.reason): 1}, outcomes(t, reader))
		})
	}
}

// TestReconcileCancelledNotCounted checks a pass cut off by shutdown is not
// counted, even when the client wraps the cancellation in a network error that
// would otherwise read as an outage.
func TestReconcileCancelledNotCounted(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	_, reconciler := newPassTest(t, c, passTest{
		perr:    &url.Error{Op: "Get", URL: "https://api", Err: context.Canceled},
		polling: true,
		prior:   provisioned(),
		options: []manager.ReconcilerOption{manager.WithTransient(), manager.WithMeterProvider(provider)},
	})

	_, _ = reconciler.Reconcile(t.Context(), newRequest(testNamespace, testName))

	assert.Empty(t, outcomes(t, reader))
}
