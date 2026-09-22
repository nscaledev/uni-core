/*
Copyright 2024-2025 the Unikorn Authors.
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

package cors

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/middleware/routeresolver"
	"github.com/unikorn-cloud/core/pkg/util"
)

// allowAllOrigins is the wildcard the CORS specification defines for "any
// origin", as distinct from an origin pattern that merely contains a "*".
const allowAllOrigins = "*"

type Options struct {
	AllowedOrigins []string
	MaxAge         int
}

func (o *Options) AddFlags(f *pflag.FlagSet) {
	f.StringSliceVar(&o.AllowedOrigins, "cors-allow-origin", []string{"*"}, "CORS allowed origins, each optionally containing a single \"*\" wildcard matching zero or more characters, e.g. https://*.example.com")
	f.IntVar(&o.MaxAge, "cors-max-age", 86400, "CORS maximum age (may be overridden by the browser)")
}

// wildcardOrigin is an allowed origin containing a "*", split around it.  A
// candidate matches when it starts with the prefix and ends with the suffix,
// so "https://*.example.com" admits "https://a.example.com" but not
// "https://a.example.com.attacker.test", whose suffix differs.
type wildcardOrigin struct {
	prefix string
	suffix string
}

func (o *wildcardOrigin) matches(origin string) bool {
	// Reject candidates too short to hold both halves, otherwise the prefix and
	// suffix could satisfy themselves from overlapping text: "abc*bcd" would
	// match "abcd".
	if len(origin) < len(o.prefix)+len(o.suffix) {
		return false
	}

	return strings.HasPrefix(origin, o.prefix) && strings.HasSuffix(origin, o.suffix)
}

type CORS struct {
	options *Options

	// wildcards holds the parsed form of every entry in the options that
	// contains a "*", precomputed here so matching stays allocation free.
	wildcards []wildcardOrigin
}

func New(options *Options) *CORS {
	c := &CORS{
		options: options,
	}

	for _, origin := range options.AllowedOrigins {
		// A bare "*" keeps its established "allow anything" meaning and is
		// emitted verbatim by the fallback below, so it is not a wildcard.
		if origin == allowAllOrigins {
			continue
		}

		if prefix, suffix, found := strings.Cut(origin, "*"); found {
			c.wildcards = append(c.wildcards, wildcardOrigin{prefix: prefix, suffix: suffix})
		}
	}

	return c
}

func (c *CORS) originAllowed(origin string) bool {
	if slices.Contains(c.options.AllowedOrigins, origin) {
		return true
	}

	return slices.ContainsFunc(c.wildcards, func(w wildcardOrigin) bool {
		return w.matches(origin)
	})
}

func (c *CORS) setAllowOrigin(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && c.originAllowed(origin) {
		w.Header().Add("Access-Control-Allow-Origin", origin)
		return
	}

	// Nothing matched, so fall back to the first configured origin.  A browser
	// rejects the response unless that happens to be the caller's own origin,
	// which is the intent; configure a concrete origin first so this carries a
	// usable value rather than an unmatchable wildcard pattern.
	w.Header().Add("Access-Control-Allow-Origin", c.options.AllowedOrigins[0])
}

func (c *CORS) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All requests get the allow origin header.  BUT only one!
		c.setAllowOrigin(w, r)

		// For normal requests handle them.
		if r.Method != http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Handle preflight
		method := r.Header.Get("Access-Control-Request-Method")
		if method == "" {
			errors.HandleError(w, r, errors.OAuth2InvalidRequest("OPTIONS missing Access-Control-Request-Method header"))
			return
		}

		request := r.Clone(r.Context())
		request.Method = method

		// The route resolver will have already handled the OPTIONS method
		// and translated to the correct request method for us.
		route, err := routeresolver.FromContext(r.Context())
		if err != nil {
			errors.HandleError(w, r, err)
			return
		}

		// TODO: add OPTIONS to the schema?
		methods := util.Keys(route.Route.PathItem.Operations())
		methods = append(methods, http.MethodOptions)

		// TODO: I've tried adding them to the schema, but the generator
		// adds them to the hander function signatures, which is superfluous
		// to requirements.
		headers := []string{
			"Authorization",
			"Content-Type",
			"traceparent",
			"tracestate",
		}

		w.Header().Add("Access-Control-Allow-Methods", strings.Join(methods, ", "))
		w.Header().Add("Access-Control-Allow-Headers", strings.Join(headers, ", "))
		w.Header().Add("Access-Control-Max-Age", strconv.Itoa(c.options.MaxAge))
		w.WriteHeader(http.StatusNoContent)
	})
}
