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

package options_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	"github.com/unikorn-cloud/core/pkg/options"

	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

type otlpCollector struct {
	mu      sync.Mutex
	metrics []*collectormetrics.ExportMetricsServiceRequest
	server  *httptest.Server
}

func newOTLPCollector() *otlpCollector {
	c := &otlpCollector{}
	c.server = httptest.NewServer(http.HandlerFunc(c.handle))

	return c
}

func (c *otlpCollector) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/metrics" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	req := &collectormetrics.ExportMetricsServiceRequest{}
	if err := proto.Unmarshal(body, req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	c.metrics = append(c.metrics, req)
	c.mu.Unlock()

	resp, _ := proto.Marshal(&collectormetrics.ExportMetricsServiceResponse{})

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func (c *otlpCollector) endpoint() string {
	return strings.TrimPrefix(c.server.URL, "http://")
}

func (c *otlpCollector) metricNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var names []string

	for _, req := range c.metrics {
		for _, rm := range req.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					names = append(names, m.GetName())
				}
			}
		}
	}

	return names
}

func (c *otlpCollector) close() {
	c.server.Close()
}

func TestSetupOpenTelemetryWithoutEndpointSucceeds(t *testing.T) {
	t.Parallel()

	o := &options.CoreOptions{}

	require.NoError(t, o.SetupOpenTelemetry(t.Context()))

	assert.NotNil(t, otel.GetMeterProvider())
	assert.NotNil(t, otel.GetTracerProvider())
}

func TestSetupOpenTelemetryBridgesControllerRuntimeMetrics(t *testing.T) {
	t.Parallel()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_bridge_verify_total",
		Help: "Synthetic metric to verify the prometheus->OTel bridge.",
	})
	require.NoError(t, ctrlmetrics.Registry.Register(counter))
	t.Cleanup(func() { ctrlmetrics.Registry.Unregister(counter) })
	counter.Inc()

	collector := newOTLPCollector()
	defer collector.close()

	o := &options.CoreOptions{OTLPEndpoint: collector.endpoint()}
	require.NoError(t, o.SetupOpenTelemetry(t.Context()))

	provider, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider)
	require.True(t, ok)
	require.NoError(t, provider.Shutdown(t.Context()))

	assert.Contains(t, collector.metricNames(), "test_bridge_verify_total")
}
