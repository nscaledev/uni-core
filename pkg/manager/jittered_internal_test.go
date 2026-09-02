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

// This file is deliberately in package manager rather than manager_test: the
// jitter arithmetic is worth testing directly, and driving it through Reconcile
// would need a mock round trip per sample.
package manager

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestJitteredParks checks the non-polling case is untouched.  A zero duration
// must stay exactly zero, because controller-runtime reads that as "done" and it
// is what every controller that has not opted into polling relies on.
func TestJitteredParks(t *testing.T) {
	t.Parallel()

	assert.Zero(t, jittered(0))
}

// TestJitteredTooSmallToDivide checks the lower guard.  rand.N panics on a
// non-positive argument, and any duration below requeueJitterFraction
// nanoseconds divides to zero.  Absurd as a real configuration, but a panic is
// not an acceptable response to one.
func TestJitteredTooSmallToDivide(t *testing.T) {
	t.Parallel()

	d := time.Duration(requeueJitterFraction - 1)

	assert.NotPanics(t, func() {
		assert.Equal(t, d, jittered(d))
	})
}

// TestJitteredNearOverflow checks the upper guard, which is the interesting one:
// adding jitter to a duration near the end of the int64 range wraps to a
// negative, and controller-runtime reads a non-positive RequeueAfter as "done".
// So without this an absurd period would silently stop the very polling it
// configured - the failure mode the WithPolling fallback exists to prevent.
// time.ParseDuration accepts durations of this size, so it is reachable from the
// command line.
func TestJitteredNearOverflow(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{math.MaxInt64, math.MaxInt64 - 1, 2400000 * time.Hour} {
		got := jittered(d)

		assert.Positive(t, got, "jitter must never produce a non-positive duration")
		assert.GreaterOrEqual(t, got, d)
	}
}

// TestJitteredInWindow checks every sample lands in [d, d+10%).  The duration is
// a floor: jitter is added, never subtracted, so a caller asking for a minute is
// never woken more often than once a minute.
func TestJitteredInWindow(t *testing.T) {
	t.Parallel()

	const d = time.Minute

	for range 1000 {
		got := jittered(d)

		assert.GreaterOrEqual(t, got, d)
		assert.Less(t, got, d+d/requeueJitterFraction)
	}
}

// TestJitteredSpreads is the test that actually justifies the change: the values
// must differ, or the fleet stays in the convoy the jitter exists to break up.
// A fixed implementation would pass every other test in this file.
func TestJitteredSpreads(t *testing.T) {
	t.Parallel()

	seen := map[time.Duration]struct{}{}

	for range 100 {
		seen[jittered(time.Minute)] = struct{}{}
	}

	// Deliberately weak: this asserts the values are not constant, not that they
	// are well distributed.  Testing the quality of the standard library's
	// generator is not this package's job, and a tight bound here would be flaky.
	assert.Greater(t, len(seen), 1)
}
