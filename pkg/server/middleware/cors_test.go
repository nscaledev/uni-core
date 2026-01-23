/*
Copyright 2025 the Unikorn Authors.
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

package middleware_test

import (
	_ "embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/openapi/helpers"
	"github.com/unikorn-cloud/core/pkg/server/middleware/cors"
	"github.com/unikorn-cloud/core/pkg/server/middleware/routeresolver"
)

const (
	origin  = "www.acme.com"
	path    = "/api"
	badPath = "/missing"
)

//go:embed cors_test.schema.yaml
var schema []byte

func getSchema(t *testing.T) *helpers.Schema {
	t.Helper()

	s, err := openapi3.NewLoader().LoadFromData(schema)
	require.NoError(t, err)

	getter := func() (*openapi3.T, error) {
		return s, nil
	}

	schema, err := helpers.NewSchema(getter)
	require.NoError(t, err)

	return schema
}

func getOptions(t *testing.T, allowedOrigins ...string) *cors.Options {
	t.Helper()

	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{
			"*",
		}
	}

	return &cors.Options{
		AllowedOrigins: allowedOrigins,
	}
}

func getHandler(t *testing.T, options *cors.Options) http.Handler {
	t.Helper()

	routeresolver := routeresolver.New(getSchema(t))
	cors := cors.New(options)

	r := chi.NewRouter()
	r.Use(routeresolver.Middleware)
	r.Use(cors.Middleware)

	r.Group(func(r chi.Router) {
		r.Get("/api", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	})

	return r
}

func defaultRequestWithOrigin(t *testing.T, origin string) *http.Request {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, path, nil)
	r.Header.Add("Origin", origin)
	r.Header.Add("Access-Control-Request-Method", "GET")

	return r
}

func defaultExpectedHeadersWithOrigin(t *testing.T, origin string) http.Header {
	t.Helper()

	header := http.Header{}
	header.Add("Access-Control-Allow-Origin", origin)
	header.Add("Access-Control-Allow-Methods", "GET, OPTIONS")
	header.Add("Access-Control-Allow-Headers", "Authorization, Content-Type, traceparent, tracestate")
	header.Add("Access-Control-Max-Age", "0")

	return header
}

// TestCORS checks default operation.
func TestCORS(t *testing.T) {
	t.Parallel()

	handler := getHandler(t, getOptions(t))

	w := httptest.NewRecorder()

	handler.ServeHTTP(w, defaultRequestWithOrigin(t, origin))
	require.Equal(t, http.StatusNoContent, w.Code)

	require.Equal(t, defaultExpectedHeadersWithOrigin(t, "*"), w.Header())
}

// TestCORSExplicitOriginHit checks operation with an explcit set of allowed origins.
func TestCORSExplicitOriginHit(t *testing.T) {
	t.Parallel()

	origin1 := "foo.acme.com"
	origin2 := "bar.acme.com"
	origin3 := "baz.acme.com"

	handler := getHandler(t, getOptions(t, origin1, origin2, origin3))

	w := httptest.NewRecorder()

	handler.ServeHTTP(w, defaultRequestWithOrigin(t, origin2))
	require.Equal(t, http.StatusNoContent, w.Code)

	require.Equal(t, defaultExpectedHeadersWithOrigin(t, origin2), w.Header())
}

// TestCORSExplicitOriginMiss checks operation with an explcit set of allowed origins.
func TestCORSExplicitOriginMiss(t *testing.T) {
	t.Parallel()

	origin1 := "foo.acme.com"
	origin2 := "bar.acme.com"
	origin3 := "baz.acme.com"

	handler := getHandler(t, getOptions(t, origin1, origin2, origin3))

	w := httptest.NewRecorder()

	handler.ServeHTTP(w, defaultRequestWithOrigin(t, origin))
	require.Equal(t, http.StatusNoContent, w.Code)

	require.Equal(t, defaultExpectedHeadersWithOrigin(t, origin1), w.Header())
}

// TestCORSBadRequestMethod checks the Access-Control-Request-Method is required.
func TestCORSBadRequestMethod(t *testing.T) {
	t.Parallel()

	handler := getHandler(t, getOptions(t))

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, badPath, nil)
	r.Header.Add("Origin", origin)

	handler.ServeHTTP(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
