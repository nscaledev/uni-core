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
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// outcomeScope is the instrumentation scope of the reconcile outcome counter.
const outcomeScope = "github.com/unikorn-cloud/core/pkg/manager"

// newOutcomes creates the reconcile outcome counter.  See the README's
// "Invariants And Guard Rails" section.
func newOutcomes(provider metric.MeterProvider) metric.Int64Counter {
	counter, err := provider.Meter(outcomeScope).Int64Counter("reconcile_outcomes",
		metric.WithUnit("{pass}"),
		metric.WithDescription("Reconcile passes, by what each one returned."))
	if err != nil {
		log.Log.WithName("manager").Error(err, "reconcile outcomes will not be counted")

		return noop.Int64Counter{}
	}

	return counter
}

// outcome names what a pass returned.  Every label value is from a fixed set.
func outcome(err error, transient bool) string {
	switch {
	case err == nil:
		return "success"
	case transient:
		return "transient"
	case errors.Is(err, provisioners.ErrRecheck):
		return "recheck"
	case errors.Is(err, provisioners.ErrYield):
		return "yield"
	case errors.Is(err, provisioners.ErrUserActionRequired):
		return "user_action_required"
	case errors.Is(err, provisioners.ErrTerminal):
		return "terminal"
	default:
		return "error"
	}
}

// countOutcome counts a pass.  The reason is the one written to the condition,
// or empty when the condition was left alone.
func (r *Reconciler) countOutcome(ctx context.Context, err error, transient, deprovision bool, reason unikornv1.ProvisioningConditionReason) {
	operation := "provision"
	if deprovision {
		operation = "deprovision"
	}

	r.outcomes.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.String("outcome", outcome(err, transient)),
		attribute.String("reason", string(reason)),
	))
}
