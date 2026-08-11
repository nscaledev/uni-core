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

package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/server/middleware/cors"
)

// allowOriginFor runs a non-preflight request through the middleware and
// returns the Access-Control-Allow-Origin header it set.
func allowOriginFor(t *testing.T, allowedOrigins []string, origin string) string {
	t.Helper()

	middleware := cors.New(&cors.Options{AllowedOrigins: allowedOrigins})

	var called bool

	handler := middleware.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	// A non-OPTIONS request must always reach the wrapped handler, whether or
	// not the origin was allowed; CORS is enforced by the browser.
	require.True(t, called)

	return recorder.Header().Get("Access-Control-Allow-Origin")
}

func TestSetAllowOriginExactMatch(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://console.example.com", "http://localhost:3000"}

	assert.Equal(t, "https://console.example.com", allowOriginFor(t, allowed, "https://console.example.com"))
	assert.Equal(t, "http://localhost:3000", allowOriginFor(t, allowed, "http://localhost:3000"))
}

func TestSetAllowOriginWildcardMatch(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://console.example.com", "https://*.preview.example.com"}

	for _, origin := range []string{
		"https://pr-1234.preview.example.com",
		"https://feat-billing.preview.example.com",
		// A "*" matches zero or more characters, including dots, so nested
		// labels are admitted too.
		"https://a.b.preview.example.com",
		// Zero characters is a match, as it is for any glob.  No browser emits
		// an origin with an empty label, so this is a curiosity rather than a
		// hole; it is pinned here so the behaviour is deliberate.
		"https://.preview.example.com",
	} {
		assert.Equal(t, origin, allowOriginFor(t, allowed, origin), origin)
	}
}

// TestSetAllowOriginWildcardRejects covers the cases a naive substring or
// suffix match would wrongly admit.  Each must fall back to the first
// configured origin rather than echo the caller's.
func TestSetAllowOriginWildcardRejects(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://console.example.com", "https://*.preview.example.com"}

	for _, origin := range []string{
		// An attacker-controlled domain that merely ends with the pattern's
		// suffix nowhere near the right position.
		"https://evil.example.com",
		// Suffix-extension: the allowed host appears, but the real origin is
		// the attacker's registrable domain.
		"https://x.preview.example.com.attacker.test",
		// Scheme downgrade: the prefix includes "https://".
		"http://pr-1234.preview.example.com",
		// The bare apex has no label to satisfy the pattern's literal dot.
		"https://preview.example.com",
	} {
		assert.Equal(t, "https://console.example.com", allowOriginFor(t, allowed, origin), origin)
	}
}

// TestSetAllowOriginWildcardOverlap covers a candidate short enough that the
// prefix and suffix would otherwise match overlapping text.
func TestSetAllowOriginWildcardOverlap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc*bcd", allowOriginFor(t, []string{"abc*bcd"}, "abcd"))
	assert.Equal(t, "abcbcd", allowOriginFor(t, []string{"abc*bcd"}, "abcbcd"))
}

// TestSetAllowOriginBareWildcardUnchanged pins the pre-existing behaviour of
// the "*" flag default: it is emitted verbatim, not echoed back as the
// caller's origin, because "*" and a credentialed response are incompatible.
func TestSetAllowOriginBareWildcardUnchanged(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "*", allowOriginFor(t, []string{"*"}, "https://anything.example.com"))
	assert.Equal(t, "*", allowOriginFor(t, []string{"*"}, ""))
}

// TestSetAllowOriginFallback pins the fallback for a request that either
// carries no Origin or an origin that matches nothing.
func TestSetAllowOriginFallback(t *testing.T) {
	t.Parallel()

	allowed := []string{"https://console.example.com", "https://*.preview.example.com"}

	assert.Equal(t, "https://console.example.com", allowOriginFor(t, allowed, ""))
	assert.Equal(t, "https://console.example.com", allowOriginFor(t, allowed, "https://unknown.example.com"))
}

// TestSetAllowOriginSingleHeader guards the "BUT only one!" invariant: a
// browser rejects a response carrying multiple allow-origin values.
func TestSetAllowOriginSingleHeader(t *testing.T) {
	t.Parallel()

	middleware := cors.New(&cors.Options{
		AllowedOrigins: []string{"https://console.example.com", "https://*.preview.example.com"},
	})

	handler := middleware.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	request.Header.Set("Origin", "https://pr-1234.preview.example.com")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	assert.Len(t, recorder.Header().Values("Access-Control-Allow-Origin"), 1)
}
