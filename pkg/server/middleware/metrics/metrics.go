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

// Package metrics records request rate, errors and duration (RED) for API
// servers.
//
// Install the middleware immediately before routeresolver:
//
//	m, err := metrics.New(otel.Meter(constants.Application))
//	...
//	router.Use(opentelemetry.New(serviceName, version).Middleware)
//	router.Use(logging.New().Middleware)
//	router.Use(m.Middleware)
//	router.Use(routeresolver.New(schema).Middleware)
//	router.Use(cors.New(corsOptions).Middleware)
//
// The resolver answers requests it cannot resolve without calling the next
// handler, so the middleware has to run before it to count them. It learns
// the resolved route through routeresolver.WithResolvedHook, and labels by the
// OpenAPI route template so path parameters never become label values.
package metrics

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/unikorn-cloud/core/pkg/server/middleware/routeresolver"
)

// The names follow the OpenTelemetry HTTP server semantic conventions in
// their Prometheus-stored form, so every service emitting them lands on the
// same stored names and dashboards can aggregate across services. The emitting
// service is identified by the service label options.SetupMetrics adds to
// every scraped series, not by the metric name.
const (
	requestDurationName = "http_server_request_duration_seconds"
	activeRequestsName  = "http_server_active_requests"
)

const (
	// unmatchedRoute is the route label for requests that match no route in
	// the OpenAPI schema. Schema paths start with "/", so it cannot collide.
	unmatchedRoute = "unmatched"

	// otherMethod is the method label for any method net/http has no
	// constant for, which keeps the label bounded. The value is the one the
	// OpenTelemetry semantic conventions use for http.request.method.
	otherMethod = "_OTHER"
)

func requestDurationBounds() []float64 {
	return []float64{
		0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
	}
}

// Metrics records the bounded API request metrics required by the platform.
// The duration histogram's count serves as the request counter.
type Metrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

// New creates the API request metric instruments.
func New(meter metric.Meter) (*Metrics, error) {
	duration, err := meter.Float64Histogram(
		requestDurationName,
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(requestDurationBounds()...),
	)
	if err != nil {
		return nil, fmt.Errorf("create request duration histogram: %w", err)
	}

	active, err := meter.Int64UpDownCounter(activeRequestsName, metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create active request counter: %w", err)
	}

	return &Metrics{
		duration: duration,
		active:   active,
	}, nil
}

func boundedMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return method
	}

	return otherMethod
}

func statusClass(status int) string {
	return strconv.Itoa(status/100) + "xx"
}

// Middleware records every request. Requests that fail route resolution are
// recorded in the duration histogram under unmatchedRoute; they are answered
// by the resolver straight away, so they are not counted as active.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		attributes := make([]attribute.KeyValue, 2, 3)
		attributes[0] = attribute.String("route", unmatchedRoute)
		attributes[1] = attribute.String("method", boundedMethod(r.Method))

		var active metric.MeasurementOption

		// The hook stays in the context, so a second resolver further down the
		// chain calls it again; count the request once.
		hook := func(info *routeresolver.RouteInfo) {
			if active != nil {
				return
			}

			attributes[0] = attribute.String("route", info.Route.Path)
			active = metric.WithAttributes(attributes...)
			m.active.Add(ctx, 1, active)
		}

		defer func() {
			if active != nil {
				m.active.Add(ctx, -1, active)
			}
		}()

		started := time.Now()
		response := httpsnoop.CaptureMetrics(next, w, r.WithContext(routeresolver.WithResolvedHook(ctx, hook)))
		attributes = append(attributes, attribute.String("status_class", statusClass(response.Code)))
		m.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attributes...))
	})
}
