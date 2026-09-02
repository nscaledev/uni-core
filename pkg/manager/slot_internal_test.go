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

// In package manager so the slot arithmetic can be driven with a fixed clock.
package manager

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/types"
)

const testUID = types.UID("6f1c2a5e-6d0b-4f3e-9a51-0c6b8d2e7f41")

//nolint:gochecknoglobals
var testNow = time.Date(2026, 10, 20, 12, 34, 56, 789, time.UTC)

// TestNextSlotParks checks a non-positive period comes back unchanged, because
// controller-runtime reads zero as "done" and every non-polling controller
// relies on it.
func TestNextSlotParks(t *testing.T) {
	t.Parallel()

	assert.Zero(t, nextSlot(testUID, 0, testNow))
	assert.Equal(t, -time.Second, nextSlot(testUID, -time.Second, testNow))
}

// TestNextSlotInWindow checks the wait is never zero, which would park a
// polling controller, never under a tenth of the period, and never more than
// a period and a tenth.  The tenth is written out, not read from the constant,
// so the test pins the documented value.
func TestNextSlotInWindow(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{1, 7, time.Second, time.Minute} {
		for i := range 1000 {
			now := testNow.Add(time.Duration(i) * 7919 * time.Millisecond)

			got := nextSlot(testUID, d, now)

			assert.Positive(t, got)
			assert.GreaterOrEqual(t, got, d/10)
			assert.LessOrEqual(t, got, d+d/10)
		}
	}
}

// TestNextSlotBeforeEpoch checks a clock set before 1970, where UnixNano is
// negative, still gives a wait in the window and the same slot.
func TestNextSlotBeforeEpoch(t *testing.T) {
	t.Parallel()

	const d = time.Minute

	phase := func(t time.Time) time.Duration {
		return ((time.Duration(t.UnixNano()) % d) + d) % d
	}

	want := phase(testNow.Add(nextSlot(testUID, d, testNow)))

	for i := range 1000 {
		now := time.Unix(-1, 0).Add(-time.Duration(i) * 7919 * time.Millisecond)

		got := nextSlot(testUID, d, now)

		assert.GreaterOrEqual(t, got, d/10)
		assert.LessOrEqual(t, got, d+d/10)
		assert.Equal(t, want, phase(now.Add(got)))
	}
}

// TestNextSlotNearOverflow checks an absurd period cannot wrap to a negative,
// which controller-runtime would read as "done".  The clock is set a nanosecond
// before the slot, so the wait is under the gap and the skip is attempted.
func TestNextSlotNearOverflow(t *testing.T) {
	t.Parallel()

	const d = time.Duration(math.MaxInt64)

	// From the epoch the phase is zero, so the slot is within the range
	// UnixNano can represent.
	epoch := time.Unix(0, 0)
	slot := epoch.Add(nextSlot(testUID, d, epoch))

	assert.Equal(t, time.Duration(1), nextSlot(testUID, d, slot.Add(-1)))
}

// TestNextSlotMinimumGap checks a pass that ends just before its slot skips to
// the next one rather than running again straight away, and that the cut off
// is exactly a tenth of the period.
func TestNextSlotMinimumGap(t *testing.T) {
	t.Parallel()

	const d = 10 * time.Second

	slot := testNow.Add(nextSlot(testUID, d, testNow))

	assert.Equal(t, 10*time.Second+time.Millisecond, nextSlot(testUID, d, slot.Add(-time.Millisecond)))
	assert.Equal(t, time.Second, nextSlot(testUID, d, slot.Add(-time.Second)))
	assert.Equal(t, 11*time.Second-time.Nanosecond, nextSlot(testUID, d, slot.Add(-time.Second+time.Nanosecond)))
}

// TestNextSlotSteadyState checks that a pass run at its slot schedules the next
// one exactly a period later, and that the slot is the same from any clock
// reading, which is what lets it survive a restart or a leader change.
func TestNextSlotSteadyState(t *testing.T) {
	t.Parallel()

	const d = time.Minute

	slot := testNow.Add(nextSlot(testUID, d, testNow))

	assert.Equal(t, d, nextSlot(testUID, d, slot))

	phase := func(t time.Time) time.Duration {
		return time.Duration(t.UnixNano()) % d
	}

	for i := range 100 {
		now := testNow.Add(time.Duration(i) * 7919 * time.Millisecond)

		assert.Equal(t, phase(slot), phase(now.Add(nextSlot(testUID, d, now))))
	}
}

// TestNextSlotSpreads checks a fleet woken at the same instant is spread across
// the whole period in one cycle, rather than staying bunched.  It is the only
// test that catches every UID landing in the same slot, from a zero offset or
// a constant hash input.
func TestNextSlotSpreads(t *testing.T) {
	t.Parallel()

	const (
		d       = time.Minute
		fleet   = 1000
		buckets = 10
	)

	counts := make([]int, buckets)

	for i := range fleet {
		uid := types.UID(fmt.Sprintf("resource-%d", i))

		slot := testNow.Add(nextSlot(uid, d, testNow))

		counts[time.Duration(slot.UnixNano())%d*buckets/d]++
	}

	// Deterministic, so the bound can be tight without flaking: each tenth of
	// the period holds roughly a tenth of the fleet.
	for i, n := range counts {
		assert.Greater(t, n, fleet/buckets/2, "bucket %d", i)
	}
}
