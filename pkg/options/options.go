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
	"flag"
	"time"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"

	klog "k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// CoreOptions are things all controllers, message consumers and servers will need.
// There is a corresponding Helm include that matches this type.
type CoreOptions struct {
	// Namespace is the namespace we are running in.
	Namespace string
	// OTLPEndpoint is used by OpenTelemetry.
	OTLPEndpoint string
	// Zap controls common logging.
	Zap zap.Options
}

func (o *CoreOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace the process is running in.")
	flags.StringVar(&o.OTLPEndpoint, "otlp-endpoint", "", "An optional OTLP endpoint.")

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

func (o *CoreOptions) SetupOpenTelemetry(ctx context.Context, opts ...trace.TracerProviderOption) error {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	if o.OTLPEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(o.OTLPEndpoint), otlptracehttp.WithInsecure())
		if err != nil {
			return err
		}

		opts = append(opts, trace.WithBatcher(exporter))
	}

	otel.SetTracerProvider(trace.NewTracerProvider(opts...))

	return nil
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
