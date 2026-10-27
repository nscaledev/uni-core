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

package options

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/protobuf/proto"

	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	freshProcessEnv = "OPTIONS_TEST_FRESH_PROCESS"
	testService     = "unikorn-test"
)

// inFreshProcess reruns the calling test alone in a new test process, because
// metrics setup is once per process. It returns true in that process, where
// the test body should run.
func inFreshProcess(t *testing.T) bool {
	t.Helper()

	if os.Getenv(freshProcessEnv) == t.Name() {
		return true
	}

	//nolint:gosec
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), freshProcessEnv+"="+t.Name())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "--- PASS: "+t.Name())

	return false
}

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

// The push provider sends its own instruments and a copy of the registry.
func TestNewPushProviderPushesInstrumentsAndRegistry(t *testing.T) {
	t.Parallel()

	registry := promclient.NewRegistry()

	counter := promclient.NewCounter(promclient.CounterOpts{Name: "test_bridge_total", Help: "Test."})
	require.NoError(t, registry.Register(counter))
	counter.Inc()

	collector := newOTLPCollector()
	defer collector.close()

	provider, err := newPushProvider(t.Context(), collector.endpoint(), registry)
	require.NoError(t, err)

	instrument, err := provider.Meter("options-test").Int64Counter("test_push_instrument")
	require.NoError(t, err)
	instrument.Add(t.Context(), 1)

	require.NoError(t, provider.Shutdown(t.Context()))

	names := collector.metricNames()
	assert.Contains(t, names, "test_bridge_total")
	assert.Contains(t, names, "test_push_instrument")
}

// gatheredNames returns the metric family names in registry, i.e. the names a
// scrape stores.
func gatheredNames(t *testing.T, registry promclient.Gatherer) []string {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)

	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}

	return names
}

// Services name their instruments with the Prometheus name they expect to be
// stored. The exporter keeps those names: counters gain _total, and
// instruments without a unit get no unit suffix.
func TestNewScrapeProviderKeepsInstrumentNames(t *testing.T) {
	t.Parallel()

	provider, registry, err := newScrapeProvider(promclient.NewRegistry(), testService)
	require.NoError(t, err)

	meter := provider.Meter("options-test")

	counter, err := meter.Int64Counter("test_scrape_requests")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)

	histogram, err := meter.Float64Histogram("test_scrape_duration_seconds")
	require.NoError(t, err)
	histogram.Record(t.Context(), 1)

	_, err = meter.Int64ObservableGauge("test_scrape_state", metric.WithInt64Callback(
		func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(1)
			return nil
		}))
	require.NoError(t, err)

	names := gatheredNames(t, registry)
	assert.Contains(t, names, "test_scrape_requests_total")
	assert.Contains(t, names, "test_scrape_duration_seconds")
	assert.Contains(t, names, "test_scrape_state")
}

// Dashboards select services by the literal label service="<name>", on
// controller-runtime's own metrics as well as OTel instruments. A series that
// already has a service label keeps it.
func TestNewScrapeProviderLabelsSeriesWithService(t *testing.T) {
	t.Parallel()

	inner := promclient.NewRegistry()

	// Registered before setup, as controller-runtime registers its metrics.
	registered := promclient.NewCounter(promclient.CounterOpts{Name: "test_label_registered_total", Help: "Test."})
	require.NoError(t, inner.Register(registered))
	registered.Inc()

	existing := promclient.NewCounterVec(promclient.CounterOpts{Name: "test_label_existing_total", Help: "Test."}, []string{"service"})
	require.NoError(t, inner.Register(existing))
	existing.WithLabelValues("other").Inc()

	provider, registry, err := newScrapeProvider(inner, testService)
	require.NoError(t, err)

	instrument, err := provider.Meter("options-test").Int64Counter("test_label_instrument")
	require.NoError(t, err)
	instrument.Add(t.Context(), 1)

	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))

	body := recorder.Body.String()
	assert.Contains(t, body, "\ntest_label_registered_total{service=\"unikorn-test\"} 1\n")
	assert.Contains(t, body, "\ntest_label_existing_total{service=\"other\"} 1\n")
	assert.Regexp(t, `\ntest_label_instrument_total\{[^}]*service="unikorn-test"[^}]*\} 1\n`, body)
}

// Charts set OTEL_SERVICE_NAME per binary, which the default resource picks
// up. The service name given in code replaces it, so every binary in a service
// shares one trace service name.
func TestServiceResourceReplacesServiceName(t *testing.T) {
	t.Parallel()

	base := resource.NewSchemaless(semconv.ServiceName("unikorn-test-binary"))

	named, err := serviceResource(base, testService)
	require.NoError(t, err)

	name, ok := named.Set().Value(semconv.ServiceNameKey)
	require.True(t, ok)
	assert.Equal(t, testService, name.AsString())
}

// SetupMetrics installs the scraping provider, and replaces the
// controller-runtime registry with one that labels it. Instruments would be
// split by a second provider, and a second exporter on the registry would make
// every scrape fail with duplicate series, so further setup fails.
func TestSetupMetricsInstallsOnce(t *testing.T) {
	t.Parallel()

	if !inFreshProcess(t) {
		return
	}

	registered := promclient.NewCounter(promclient.CounterOpts{Name: "test_setup_registered_total", Help: "Test."})
	require.NoError(t, ctrlmetrics.Registry.Register(registered))
	registered.Inc()

	o := &CoreOptions{}
	require.NoError(t, o.SetupMetrics(testService))

	provider := otel.GetMeterProvider()

	counter, err := otel.Meter("options-test").Int64Counter("test_setup_requests")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)

	require.Error(t, o.SetupMetrics(testService))
	require.Error(t, o.SetupOpenTelemetry(t.Context()))
	assert.Same(t, provider, otel.GetMeterProvider())

	body := scrape(t)
	assert.Contains(t, body, "\ntest_setup_registered_total{service=\"unikorn-test\"} 1\n")
	assert.Regexp(t, `\ntest_setup_requests_total\{[^}]*service="unikorn-test"[^}]*\} 1\n`, body)
}

// SetupOpenTelemetry installs a provider that pushes to the configured
// endpoint, with a copy of the controller-runtime registry. Further setup fails.
func TestSetupOpenTelemetryInstallsOnce(t *testing.T) {
	t.Parallel()

	if !inFreshProcess(t) {
		return
	}

	registered := promclient.NewCounter(promclient.CounterOpts{Name: "test_setup_registered_total", Help: "Test."})
	require.NoError(t, ctrlmetrics.Registry.Register(registered))
	registered.Inc()

	collector := newOTLPCollector()
	defer collector.close()

	o := &CoreOptions{OTLPEndpoint: collector.endpoint()}
	require.NoError(t, o.SetupOpenTelemetry(t.Context()))

	provider, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider)
	require.True(t, ok)

	counter, err := otel.Meter("options-test").Int64Counter("test_setup_requests")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)

	require.Error(t, o.SetupMetrics(testService))
	require.Error(t, o.SetupOpenTelemetry(t.Context()))
	assert.Same(t, provider, otel.GetMeterProvider())

	require.NoError(t, provider.Shutdown(t.Context()))

	names := collector.metricNames()
	assert.Contains(t, names, "test_setup_registered_total")
	assert.Contains(t, names, "test_setup_requests")
}

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "localhost:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.NoError(t, listener.Close())

	return addr
}

// getMetrics reads addr's metrics endpoint.
func getMetrics(ctx context.Context, addr string) (int, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		return 0, "", err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, "", err
	}

	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)

	return response.StatusCode, string(data), err
}

// serveOnFreePort runs serve in the background on a free port until it serves
// metrics, and returns the port and serve's result. A released port can be
// taken again, e.g. as the local end of another test's connection, before
// serve binds it, so a serve that ends before serving is retried on another.
func serveOnFreePort(t *testing.T, serve func(addr string) error) (string, <-chan error) {
	t.Helper()

	var lastErr error

	for range 10 {
		lastErr = nil
		addr := freePort(t)
		errs := make(chan error, 1)

		go func() { errs <- serve(addr) }()

		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if len(errs) > 0 {
				lastErr = <-errs

				break
			}

			if status, _, err := getMetrics(t.Context(), addr); err == nil && status == http.StatusOK {
				return addr, errs
			}
		}

		require.Error(t, lastErr, "%s not served", addr)
	}

	require.FailNow(t, "no free port served", "last error: %v", lastErr)

	return "", nil
}

// scrape serves the controller-runtime registry with ServeMetrics and returns
// what a scrape of it reads.
func scrape(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	o := &CoreOptions{}

	addr, errs := serveOnFreePort(t, func(addr string) error { return o.ServeMetrics(ctx, addr) })

	status, body, err := getMetrics(ctx, addr)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	cancel()
	require.NoError(t, <-errs)

	return body
}

func TestServeMetricsServesControllerRuntimeRegistry(t *testing.T) {
	t.Parallel()

	counter := promclient.NewCounter(promclient.CounterOpts{
		Name: "test_serve_metrics_total",
		Help: "Synthetic metric to verify the metrics endpoint.",
	})
	require.NoError(t, ctrlmetrics.Registry.Register(counter))
	t.Cleanup(func() { ctrlmetrics.Registry.Unregister(counter) })

	assert.Contains(t, scrape(t), "test_serve_metrics_total")
}

// "0" disables the metrics server, as it does for a manager.
func TestServeMetricsDisabled(t *testing.T) {
	t.Parallel()

	o := &CoreOptions{}
	require.NoError(t, o.ServeMetrics(t.Context(), "0"))
}

var (
	errTestRun            = errors.New("test run failed")
	errTestMetricsStopped = errors.New("test metrics stopped")
)

// returned waits for the result of a call running in the background, failing
// if it does not return, i.e. something was not stopped.
func returned(t *testing.T, errs <-chan error) error {
	t.Helper()

	select {
	case err := <-errs:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, "did not return")

		return nil
	}
}

// A process whose metrics cannot be served stops: run's context is cancelled
// and the metrics error is returned.
func TestRunWithMetricsStopsRunWhenMetricsCannotBeServed(t *testing.T) {
	t.Parallel()

	taken, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "localhost:0")
	require.NoError(t, err)

	defer taken.Close()

	o := &CoreOptions{}

	errs := make(chan error, 1)

	go func() {
		errs <- o.RunWithMetrics(t.Context(), taken.Addr().String(), func(ctx context.Context) error {
			<-ctx.Done()

			return nil
		})
	}()

	require.ErrorContains(t, returned(t, errs), "address already in use")
}

// When the work ends, with or without an error, the metrics server stops too,
// so the process can exit.
func TestRunWithMetricsStopsMetricsWhenRunReturns(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{errTestRun, nil} {
		o := &CoreOptions{}

		release := make(chan struct{})

		_, errs := serveOnFreePort(t, func(addr string) error {
			return o.RunWithMetrics(t.Context(), addr, func(ctx context.Context) error {
				select {
				case <-release:
					return runErr
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		})

		close(release)

		require.ErrorIs(t, returned(t, errs), runErr)
	}
}

// Work that ends first returns its own result, not the error the metrics
// server reports on being stopped, and only once the metrics server has
// returned. The fake metrics server can only see its context done once the
// work has returned, so it reports afterwards.
func TestRunWithMetricsReturnsRunResultWhenRunEndsFirst(t *testing.T) {
	t.Parallel()

	for _, runErr := range []error{errTestRun, nil} {
		runReturned := make(chan struct{})
		metricsReturned := make(chan struct{})

		serveMetrics := func(ctx context.Context) error {
			defer close(metricsReturned)

			<-ctx.Done()

			select {
			case <-runReturned:
			default:
				t.Error("metrics stopped before run returned")
			}

			return errTestMetricsStopped
		}

		err := runWithMetrics(t.Context(), serveMetrics, func(context.Context) error {
			defer close(runReturned)

			return runErr
		})
		if runErr == nil {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, runErr)
		}

		select {
		case <-metricsReturned:
		default:
			require.FailNow(t, "metrics server outlived the work")
		}
	}
}

// Cancelling the parent context, as SIGTERM would, stops both.
func TestRunWithMetricsStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	o := &CoreOptions{}

	_, errs := serveOnFreePort(t, func(addr string) error {
		return o.RunWithMetrics(ctx, addr, func(ctx context.Context) error {
			<-ctx.Done()

			return nil
		})
	})

	cancel()

	require.NoError(t, returned(t, errs))
}

// Disabled metrics are not a failure: the work carries on alone.
func TestRunWithMetricsDisabledRunsAlone(t *testing.T) {
	t.Parallel()

	o := &CoreOptions{}

	err := o.RunWithMetrics(t.Context(), "0", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return errTestRun
		case <-time.After(200 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, err)
}

// Cancelling stops the server gracefully: it stops accepting connections, a
// request in flight still completes, and the server closing is not an error.
func TestServeHTTPShutsDownGracefully(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "localhost:0")
	require.NoError(t, err)

	addr := listener.Addr().String()

	entered := make(chan struct{})
	release := make(chan struct{})

	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.WriteHeader(http.StatusOK)
		}),
	}

	errs := make(chan error, 1)

	go func() {
		errs <- ServeHTTP(ctx, server, func() error { return server.Serve(listener) }, 5*time.Second)
	}()

	responses := make(chan int, 1)

	go func() {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/", nil)
		if err != nil {
			responses <- 0
			return
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			responses <- 0
			return
		}

		defer response.Body.Close()

		responses <- response.StatusCode
	}()

	<-entered
	cancel()

	// Shutdown closes the listener first; once it has, release the request.
	require.Eventually(t, func() bool {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
		if err != nil {
			return true
		}

		_ = conn.Close()

		return false
	}, 5*time.Second, 10*time.Millisecond)

	close(release)

	assert.Equal(t, http.StatusOK, <-responses)
	require.NoError(t, returned(t, errs))
}

func TestServeHTTPReturnsServeError(t *testing.T) {
	t.Parallel()

	err := ServeHTTP(t.Context(), &http.Server{ReadHeaderTimeout: time.Second}, func() error { return errTestRun }, time.Second)
	require.ErrorIs(t, err, errTestRun)
}

func TestSetupMetricsRequiresService(t *testing.T) {
	t.Parallel()

	o := &CoreOptions{}
	require.Error(t, o.SetupMetrics(""))
	require.Error(t, o.SetupTracing(t.Context(), ""))
}
