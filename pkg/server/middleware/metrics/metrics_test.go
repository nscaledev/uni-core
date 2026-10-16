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

package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/unikorn-cloud/core/pkg/openapi/helpers"
	"github.com/unikorn-cloud/core/pkg/server/middleware/metrics"
	"github.com/unikorn-cloud/core/pkg/server/middleware/routeresolver"
)

const schema = `
openapi: 3.0.3
info:
  title: metrics test
  version: 1.0.0
paths:
  /api/v1/projects/{projectID}:
    parameters:
    - name: projectID
      in: path
      required: true
      schema:
        type: string
    get:
      responses:
        '200': {}
`

const (
	durationName = "http_server_request_duration_seconds"
	activeName   = "http_server_active_requests"

	projectRoute = "/api/v1/projects/{projectID}"
	projectPath  = "/api/v1/projects/first"
)

// series is one stored series: its labels, and its value (a gauge value or a
// histogram sample count).
type series struct {
	labels map[string]string
	value  float64
}

// gather returns the stored series of the named family. The exporter's
// otel_scope_* labels are dropped: they come from the exporter, not from
// this package.
func gather(t *testing.T, registry *prometheus.Registry, name string, kind dto.MetricType) []series {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		require.Equal(t, kind, family.GetType(), name)

		result := make([]series, 0, len(family.GetMetric()))

		for _, m := range family.GetMetric() {
			labels := map[string]string{}

			for _, label := range m.GetLabel() {
				if !strings.HasPrefix(label.GetName(), "otel_scope_") {
					labels[label.GetName()] = label.GetValue()
				}
			}

			value := m.GetGauge().GetValue()
			if kind == dto.MetricType_HISTOGRAM {
				value = float64(m.GetHistogram().GetSampleCount())
			}

			result = append(result, series{labels: labels, value: value})
		}

		return result
	}

	t.Fatalf("family %q not found", name)

	return nil
}

// activeProjectGet is the active-request series for GET on projectRoute. The
// test schema has no other operation, and unmatched requests are never
// active, so it is the only active series the tests can produce.
func activeProjectGet(value float64) series {
	return series{labels: map[string]string{"route": projectRoute, "method": "GET"}, value: value}
}

func duration(route, method, statusClass string, count float64) series {
	return series{labels: map[string]string{"route": route, "method": method, "status_class": statusClass}, value: count}
}

// newRouter chains the middleware with the given number of route resolvers
// after it, in front of a handler that responds 503. The handler stores the
// active-request series it can see in *activeDuringRequest.
func newRouter(t *testing.T, resolvers int, activeDuringRequest *[]series) (http.Handler, *prometheus.Registry) {
	t.Helper()

	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	reporter, err := metrics.New(sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)).Meter("test"))
	require.NoError(t, err)

	document, err := openapi3.NewLoader().LoadFromData([]byte(schema))
	require.NoError(t, err)

	spec, err := helpers.NewSchema(func() (*openapi3.T, error) { return document, nil })
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(reporter.Middleware)

	for range resolvers {
		router.Use(routeresolver.New(spec).Middleware)
	}

	router.Get("/api/v1/projects/{projectID}", func(w http.ResponseWriter, _ *http.Request) {
		*activeDuringRequest = gather(t, registry, activeName, dto.MetricType_GAUGE)

		w.WriteHeader(http.StatusServiceUnavailable)
	})

	return router, registry
}

// TestStoredMetrics checks the names, labels and buckets as Prometheus stores
// them, through a real routeresolver, since dashboards query these strings.
func TestStoredMetrics(t *testing.T) {
	t.Parallel()

	var activeDuringRequest []series

	router, registry := newRouter(t, 1, &activeDuringRequest)

	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, projectPath},
		{http.MethodGet, "/api/v1/projects/second"},
		{http.MethodGet, "/missing"},
		{http.MethodDelete, projectPath},
		{"BREW", projectPath},
	}

	for _, request := range requests {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), request.method, "http://example.com"+request.path, nil))
	}

	assert.ElementsMatch(t, []series{activeProjectGet(1)}, activeDuringRequest)
	assert.ElementsMatch(t, []series{activeProjectGet(0)}, gather(t, registry, activeName, dto.MetricType_GAUGE))

	assert.ElementsMatch(t, []series{
		duration(projectRoute, "GET", "5xx", 2),
		duration("unmatched", "GET", "4xx", 1),
		duration("unmatched", "DELETE", "4xx", 1),
		duration("unmatched", "_OTHER", "4xx", 1),
	}, gather(t, registry, durationName, dto.MetricType_HISTOGRAM))

	families, err := registry.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != durationName {
			continue
		}

		var bounds []float64
		for _, bucket := range family.GetMetric()[0].GetHistogram().GetBucket() {
			bounds = append(bounds, bucket.GetUpperBound())
		}

		assert.Equal(t, []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, bounds)
	}
}

// TestRepeatedResolution checks that a request resolved more than once is
// counted as active once.
func TestRepeatedResolution(t *testing.T) {
	t.Parallel()

	var activeDuringRequest []series

	router, registry := newRouter(t, 2, &activeDuringRequest)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com"+projectPath, nil))

	assert.ElementsMatch(t, []series{activeProjectGet(1)}, activeDuringRequest)
	assert.ElementsMatch(t, []series{activeProjectGet(0)}, gather(t, registry, activeName, dto.MetricType_GAUGE))
}
