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
	"net/http"
	"sync"

	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/openapi/helpers"
	"github.com/unikorn-cloud/core/pkg/server/errors"
)

// Options provide rate limiting options to the operator.
type Options struct {
	globalRateLimitPerSecond   int64
	endpointRateLimitPerSecond int64
}

func (o *Options) AddFlags(f *pflag.FlagSet) {
	f.Int64Var(&o.globalRateLimitPerSecond, "ratelimit-rps-global", 10000, "Number of requests that can be processed per second across all APIs")
	f.Int64Var(&o.endpointRateLimitPerSecond, "ratelimit-rps-endpoint", 100, "Number of requests that can be processed per second across a single API endpoint")
}

// keyedRateLimiter allows rate limiters to be defined per-something.  These are
// typically used to prevent one actor from starving another out of the system.
// NOTE: the map used is unconstrained, so for things like individual organizations
// or users, where there are a lot of them, there will be an associated memory
// cost, so we should consider some form of garbage collection for unused limiters.
// Blacklisting persistent offenders is another option we should consider too.
// NOTE: each endpoint gets the same rate limit at present.
type keyedRateLimiter struct {
	options *Options
	m       map[string]RateLimiter
	lock    sync.Mutex
}

func newKeyedRateLimiter(options *Options) *keyedRateLimiter {
	return &keyedRateLimiter{
		options: options,
		m:       map[string]RateLimiter{},
	}
}

func (r *keyedRateLimiter) get(key string) RateLimiter {
	r.lock.Lock()
	defer r.lock.Unlock()

	rateLimiter, ok := r.m[key]
	if ok {
		return rateLimiter
	}

	rateLimiter = NewLeakyBucket(r.options.endpointRateLimitPerSecond)

	r.m[key] = rateLimiter

	return rateLimiter
}

// RateLimiterMiddleware can be added into the middleware stack by any server to
// guard against denial of service attacks on long running or computationally
// expensive APIs.  At present this has a global rate limter for the entire API
// and per-endpoint.  Each endpoint is defined as a tuple of method and path
// so there is the possibility of having different rates for GETs (which may use
// a fast read aside cache) and POSTs (which may involve talking to a database
// and message queue).
type RateLimiterMiddleware struct {
	options     *Options
	schema      *helpers.Schema
	global      RateLimiter
	perEndpoint *keyedRateLimiter
}

func New(options *Options, schema *helpers.Schema) *RateLimiterMiddleware {
	return &RateLimiterMiddleware{
		options:     options,
		schema:      schema,
		global:      NewLeakyBucket(options.globalRateLimitPerSecond),
		perEndpoint: newKeyedRateLimiter(options),
	}
}

func (m *RateLimiterMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := m.global.Request(); err != nil {
				errors.HandleError(w, r, err)
				return
			}

			// OPTIONS methods are invoked by CORS from the browser and
			// are implemented virtually by middleware, so ignore these
			// specific endpoints.
			if r.Method != http.MethodOptions {
				route, _, err := m.schema.FindRoute(r)
				if err != nil {
					errors.HandleError(w, r, err)
					return
				}

				endpointKey := route.Method + ":" + route.Path

				if err := m.perEndpoint.get(endpointKey).Request(); err != nil {
					errors.HandleError(w, r, err)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
