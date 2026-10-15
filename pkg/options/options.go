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

package options

import (
	"context"
	"errors"
	"flag"
	"slices"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/spf13/pflag"
	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
	"google.golang.org/protobuf/proto"

	klog "k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// ServiceLabel is the label SetupMetrics adds to every scraped series, naming
// the service that emitted it. Dashboards and alerts select services by it.
const ServiceLabel = "service"

var (
	errMetricsSetUp = errors.New("metrics are already set up")
	errNoService    = errors.New("a service name is required")
)

// CoreOptions are things all controllers, message consumers and servers will need.
// There is a corresponding Helm include that matches this type.
type CoreOptions struct {
	// Namespace is the namespace we are running in.
	Namespace string
	// OTLPEndpoint is used by OpenTelemetry.
	OTLPEndpoint string
	// TraceSampingRatio is the number percentage of trace samples to take
	// as a value between 0.0-1.0.
	TraceSampingRatio float64
	// Zap controls common logging.
	Zap zap.Options
}

func (o *CoreOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace the process is running in.")
	flags.StringVar(&o.OTLPEndpoint, "otlp-endpoint", "", "An optional OTLP endpoint.")
	flags.Float64Var(&o.TraceSampingRatio, "trace-sampling-ratio", 0.0, "OpenTelemetry trace sampling ratio, this affects console logging")

	z := flag.NewFlagSet("", flag.ExitOnError)
	o.Zap.BindFlags(z)

	flags.AddGoFlagSet(z)
}

func (o *CoreOptions) SetupLogging() {
	logr := zap.New(zap.UseFlagOptions(&o.Zap))

	log.SetLogger(logr)
	klog.SetLogger(logr)
	otel.SetLogger(logr)
}

// SetupTracing sets the global trace propagator and tracer provider, exporting
// traces when an OTLP endpoint is set. Traces are named by service, which
// takes precedence over OTEL_SERVICE_NAME.
func (o *CoreOptions) SetupTracing(ctx context.Context, service string, opts ...trace.TracerProviderOption) error {
	if service == "" {
		return errNoService
	}

	named, err := serviceResource(resource.Default(), service)
	if err != nil {
		return err
	}

	return o.setupTracing(ctx, append([]trace.TracerProviderOption{trace.WithResource(named)}, opts...)...)
}

// serviceResource names base by service, replacing any service name it has.
func serviceResource(base *resource.Resource, service string) (*resource.Resource, error) {
	return resource.Merge(base, resource.NewSchemaless(semconv.ServiceName(service)))
}

func (o *CoreOptions) setupTracing(ctx context.Context, opts ...trace.TracerProviderOption) error {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	if o.OTLPEndpoint != "" {
		traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(o.OTLPEndpoint), otlptracehttp.WithInsecure())
		if err != nil {
			return err
		}

		opts = append(opts, trace.WithBatcher(traceExporter))
	}

	switch {
	case o.TraceSampingRatio <= 0.0:
		opts = append(opts, trace.WithSampler(trace.NeverSample()))
	case o.TraceSampingRatio >= 1.0:
		opts = append(opts, trace.WithSampler(trace.AlwaysSample()))
	default:
		opts = append(opts, trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(o.TraceSampingRatio))))
	}

	otel.SetTracerProvider(trace.NewTracerProvider(opts...))

	return nil
}

// meterProviderOnce lets SetupOpenTelemetry or SetupMetrics install the global
// meter provider once per process. Instruments are bound to the provider
// current when they are created, so a second provider would split them.
//
//nolint:gochecknoglobals
var meterProviderOnce sync.Once

// installMeterProvider runs setup if no meter provider has been installed, and
// fails otherwise.
func installMeterProvider(setup func() error) error {
	err := errMetricsSetUp

	meterProviderOnce.Do(func() { err = setup() })

	return err
}

// SetupOpenTelemetry sets up tracing, and a meter provider that pushes OTel
// instruments and the controller-runtime metrics when an OTLP endpoint is set.
// It fails after SetupMetrics.
//
// Deprecated: Use SetupTracing and SetupMetrics, so metrics are scraped rather
// than pushed. Pushed copies lack namespace, pod and service labels and
// duplicate the scraped series.
func (o *CoreOptions) SetupOpenTelemetry(ctx context.Context, opts ...trace.TracerProviderOption) error {
	if err := o.setupTracing(ctx, opts...); err != nil {
		return err
	}

	return installMeterProvider(func() error {
		provider, err := newPushProvider(ctx, o.OTLPEndpoint, ctrlmetrics.Registry)
		if err != nil {
			return err
		}

		otel.SetMeterProvider(provider)

		return nil
	})
}

// newPushProvider returns a meter provider that, when endpoint is set, pushes
// its instruments and the series gathered from registry over OTLP.
func newPushProvider(ctx context.Context, endpoint string, registry ctrlmetrics.RegistererGatherer) (*sdkmetric.MeterProvider, error) {
	if endpoint == "" {
		return sdkmetric.NewMeterProvider(), nil
	}

	exporter, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(endpoint),
		otlpmetrichttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	bridge := prombridge.NewMetricProducer(prombridge.WithGatherer(registry))

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithProducer(bridge),
	))), nil
}

// SetupMetrics sets a global meter provider whose instruments are exported on
// the controller-runtime registry, so they are scraped along with the
// controller-runtime metrics. Nothing is pushed. Every scraped series is
// labelled with service, so call it before the registry is served.
//
// It fails if called again, or after SetupOpenTelemetry.
func (o *CoreOptions) SetupMetrics(service string) error {
	if service == "" {
		return errNoService
	}

	return installMeterProvider(func() error {
		provider, registry, err := newScrapeProvider(ctrlmetrics.Registry, service)
		if err != nil {
			return err
		}

		ctrlmetrics.Registry = registry

		otel.SetMeterProvider(provider)

		return nil
	})
}

// newScrapeProvider returns a meter provider whose instruments are gathered
// from registry, and registry wrapped to label every gathered series with
// service.
func newScrapeProvider(registry ctrlmetrics.RegistererGatherer, service string) (*sdkmetric.MeterProvider, ctrlmetrics.RegistererGatherer, error) {
	labelled := &serviceRegistry{
		RegistererGatherer: registry,
		label:              &dto.LabelPair{Name: proto.String(ServiceLabel), Value: proto.String(service)},
	}

	exporter, err := prometheus.New(prometheus.WithRegisterer(labelled))
	if err != nil {
		return nil, nil, err
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), labelled, nil
}

// serviceRegistry adds the service label to every gathered series that does
// not already have one. Controller-runtime's metrics server gathers whatever
// ctrlmetrics.Registry is when it starts, so this covers its own metrics as
// well as OTel instruments.
type serviceRegistry struct {
	ctrlmetrics.RegistererGatherer

	label *dto.LabelPair
}

func (r *serviceRegistry) Gather() ([]*dto.MetricFamily, error) {
	families, err := r.RegistererGatherer.Gather()

	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if slices.ContainsFunc(metric.GetLabel(), func(l *dto.LabelPair) bool { return l.GetName() == ServiceLabel }) {
				continue
			}

			metric.Label = append(metric.Label, r.label)
		}
	}

	return families, err
}

// ServeMetrics serves the controller-runtime registry at /metrics on
// bindAddress, as a controller-runtime manager does, for processes that don't
// run a manager. An empty address means :8080, and "0" disables the server.
// It blocks until the context is done.
func (o *CoreOptions) ServeMetrics(ctx context.Context, bindAddress string) error {
	server, err := metricsserver.NewServer(metricsserver.Options{BindAddress: bindAddress}, nil, nil)
	if err != nil {
		return err
	}

	if server == nil {
		return nil
	}

	return server.Start(ctx)
}

// ServerOptions are shared across all servers.
type ServerOptions struct {
	// ListenAddress tells the server what to listen on, you shouldn't
	// need to change this, its already non-privileged and the default
	// should be modified to avoid clashes with other services e.g prometheus.
	ListenAddress string

	// ReadTimeout defines how long before we give up on the client,
	// this should be fairly short.
	ReadTimeout time.Duration

	// ReadHeaderTimeout defines how long before we give up on the client,
	// this should be fairly short.
	ReadHeaderTimeout time.Duration

	// WriteTimeout defines how long we take to respond before we give up.
	// Ideally we'd like this to be short, but Openstack in general sucks
	// for performance.  Additionally some calls like cluster creation can
	// do a cascading create, e.g. create a default control plane, than in
	// turn creates a project.
	WriteTimeout time.Duration

	// RequestTimeout places a hard limit on all requests lengths.
	RequestTimeout time.Duration
}

func (o *ServerOptions) AddFlags(f *pflag.FlagSet) {
	f.StringVar(&o.ListenAddress, "server-listen-address", ":6080", "API listener address.")
	f.DurationVar(&o.ReadTimeout, "server-read-timeout", time.Second, "How long to wait for the client to send the request body.")
	f.DurationVar(&o.ReadHeaderTimeout, "server-read-header-timeout", time.Second, "How long to wait for the client to send headers.")
	f.DurationVar(&o.WriteTimeout, "server-write-timeout", 10*time.Second, "How long to wait for the API to respond to the client.")
	f.DurationVar(&o.RequestTimeout, "server-request-timeout", 30*time.Second, "How long to wait of a request to be serviced.")
}
