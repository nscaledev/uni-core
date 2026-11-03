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
	"time"

	"go.opentelemetry.io/otel/metric"

	"k8s.io/apimachinery/pkg/types"
)

// NextSlot exposes nextSlot to the external test package.
func NextSlot(uid types.UID, d time.Duration, now time.Time) time.Duration {
	return nextSlot(uid, d, now)
}

// WithMeterProvider counts reconcile outcomes with provider rather than the
// global one, so a test can read them.
func WithMeterProvider(provider metric.MeterProvider) ReconcilerOption {
	return func(r *Reconciler) {
		r.outcomes = newOutcomes(provider)
	}
}
