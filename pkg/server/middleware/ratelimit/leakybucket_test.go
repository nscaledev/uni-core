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

package ratelimit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/server/middleware/ratelimit"
)

func TestLeakyBucket(t *testing.T) {
	t.Parallel()

	rps := 100

	// This equates to one request every 10ms.
	b := ratelimit.NewLeakyBucket(100)

	steady := time.NewTicker(10 * time.Millisecond)
	defer steady.Stop()

	// First up test that we can support a steady stream of requests
	// at the RPS.  This should extend beyond the bucket capacity to
	// ensure it does leak.
	t.Log("Testing steady rate traffic")

	timeout := time.After(2 * time.Second)

	var done bool

	for !done {
		select {
		case <-timeout:
			done = true
		case <-steady.C:
			require.NoError(t, b.Request())
		}
	}

	// Second test we can do a burst of half the bucket capacity
	// as quickly as possible.
	t.Log("Testing burst traffic")

	for range rps >> 1 {
		require.NoError(t, b.Request())
	}

	// Third go the whole hog and trigger a rate limiting.
	t.Log("Testing rate limiting")

	var seen bool

	for range rps << 1 {
		if err := b.Request(); err != nil {
			seen = true
		}
	}

	require.True(t, seen)

	// Finally sleep to allow some capacity to be freed and try steady again.
	t.Log("Testing steady rate traffic again")

	time.Sleep(100 * time.Millisecond)

	timeout = time.After(2 * time.Second)

	done = false

	for !done {
		select {
		case <-timeout:
			done = true
		case <-steady.C:
			require.NoError(t, b.Request())
		}
	}
}
