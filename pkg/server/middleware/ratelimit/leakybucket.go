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

package ratelimit

import (
	"sync"
	"time"

	"github.com/unikorn-cloud/core/pkg/server/errors"
)

// leakyBucket implements the leaky bucket as a meter algorithm for rate limiting.
// The bucket starts empty and fills as user requests come in.  The bucket empties
// via a leak at a fixed period that is defined by the requests per second.  If the
// bucket ever becomes full, then requests are rejected with a 429.  This algorithm
// allows for bursty workloads.
// NOTE: Uber do have an implmentation, but it's blocking.  Where as this fails
// fast.  We can always drop it in quite easilt in future based on real world
// performance.
type leakyBucket struct {
	// rps is the requests per second maximum.
	rps int64
	// durationPerLeak is how long between decrements of the bucket counter.
	durationPerLeak time.Duration
	// lock is for concurrency control.
	lock sync.Mutex
	// counter is the number of requests seen in the last second.
	counter int64
	// lastLeak remembers the last request time we leaked from the bucket.
	lastLeak time.Time
}

// NewLeakyBucket creates a new leaky bucket implementation.
func NewLeakyBucket(rps int64) RateLimiter {
	return &leakyBucket{
		rps:             rps,
		durationPerLeak: time.Second / time.Duration(rps),
		lastLeak:        time.Now(),
	}
}

// Request either allows or denies the request.
func (b *leakyBucket) Request() error {
	b.lock.Lock()
	defer b.lock.Unlock()

	// Work out how long has been passed since the last leak.
	delta := time.Since(b.lastLeak)

	// Calculate how many requests we can leak from the bucket since the last
	// time we did so.
	requests := int64(delta) / int64(b.durationPerLeak)
	if requests > 0 {
		// Update the last leak, remembering the exact time when it should
		// have occurred, not this exact moment otherwise we will end up
		// having a visibly lower RPS than requested due to jitter.
		b.lastLeak = b.lastLeak.Add(delta.Truncate(b.durationPerLeak))

		// Perform the leak, constraining the counter to zero.
		b.counter -= requests
		if b.counter < 0 {
			b.counter = 0
		}
	}

	// If the bucket would have overflowed, then tell the client the limit
	// has been reached.
	if b.counter == b.rps {
		return errors.TooManyRequests()
	}

	// Finally bump the counter and allow the client access.
	b.counter++

	return nil
}
