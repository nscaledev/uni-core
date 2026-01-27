/*
Copyright 2022-2024 EscherCloud.
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

package opentelemetry

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
	"go.opentelemetry.io/otel/trace"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// logValuesFromSpan gets a generic set of key/value pairs from a span for logging.
// NOTE: please don't use periods or other characters in field names that will make
// the use of json path queries difficult.
func logValuesFromSpanContext(name string, s trace.SpanContext) []any {
	return []any{
		"spanName", name,
		"spanID", s.SpanID().String(),
		"traceID", s.TraceID().String(),
	}
}

// headerBlackList are headers we shouldn't collect, or are covered somewhere
// other than http.request.header ot http.response.header.
func headerBlackList() []string {
	return []string{
		"authorization",
		"user-agent",
	}
}

func httpHeaderAttributes(header http.Header, prefix string) []attribute.KeyValue {
	attr := make([]attribute.KeyValue, 0, len(header))

	for key, values := range header {
		normalizedKey := strings.ToLower(key)

		// DO NOT EXPOSE PRIVATE INFORMATION.
		if slices.Contains(headerBlackList(), normalizedKey) {
			continue
		}

		key := attribute.Key(prefix + "." + normalizedKey)

		if len(values) == 1 {
			attr = append(attr, key.String(values[0]))
		} else {
			attr = append(attr, key.StringSlice(values))
		}
	}

	return attr
}

// httpRequestAttributes gets all the attr it can from a request.
// This is done on a best effort basis!
//
//nolint:cyclop
func httpRequestAttributes(r *http.Request) []attribute.KeyValue {
	var attr []attribute.KeyValue

	/* Protocol Processing */
	protoVersion := strings.Split(r.Proto, "/")

	attr = append(attr, semconv.NetworkProtocolName(protoVersion[0]))
	attr = append(attr, semconv.NetworkProtocolVersion(protoVersion[1]))

	/* HTTP Processing */
	switch r.Method {
	case http.MethodConnect:
		attr = append(attr, semconv.HTTPRequestMethodConnect)
	case http.MethodDelete:
		attr = append(attr, semconv.HTTPRequestMethodDelete)
	case http.MethodGet:
		attr = append(attr, semconv.HTTPRequestMethodGet)
	case http.MethodHead:
		attr = append(attr, semconv.HTTPRequestMethodHead)
	case http.MethodOptions:
		attr = append(attr, semconv.HTTPRequestMethodOptions)
	case http.MethodPatch:
		attr = append(attr, semconv.HTTPRequestMethodPatch)
	case http.MethodPost:
		attr = append(attr, semconv.HTTPRequestMethodPost)
	case http.MethodPut:
		attr = append(attr, semconv.HTTPRequestMethodPut)
	case http.MethodTrace:
		attr = append(attr, semconv.HTTPRequestMethodTrace)
	default:
		attr = append(attr, semconv.HTTPRequestMethodOther)
	}

	attr = append(attr, semconv.HTTPRequestBodySize(int(r.ContentLength)))
	attr = append(attr, httpHeaderAttributes(r.Header, "http.request.header")...)

	// User Agent Processing.
	if userAgent := r.UserAgent(); userAgent != "" {
		attr = append(attr, semconv.UserAgentOriginal(userAgent))
	}

	/* URL Processing */
	scheme := "http"

	if r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}

	attr = append(attr, semconv.URLScheme(scheme))
	attr = append(attr, semconv.URLPath(r.URL.Path))

	if r.URL.RawQuery != "" {
		attr = append(attr, semconv.URLQuery(r.URL.RawQuery))
	}

	if r.URL.Fragment != "" {
		attr = append(attr, semconv.URLFragment(r.URL.Fragment))
	}

	/* Server processing */
	serverHostPort := strings.Split(r.URL.Host, ":")

	serverPort := 80

	if len(serverHostPort) > 1 {
		t, err := strconv.Atoi(serverHostPort[1])
		if err == nil {
			serverPort = t
		}
	}

	attr = append(attr, semconv.ServerAddress(serverHostPort[0]))
	attr = append(attr, semconv.ServerPort(serverPort))

	/* Client processing */
	clientHostPort := strings.Split(r.RemoteAddr, ":")
	attr = append(attr, semconv.ClientAddress(clientHostPort[0]))

	if clientPort, err := strconv.Atoi(clientHostPort[1]); err == nil {
		attr = append(attr, semconv.ClientPort(clientPort))
	}

	return attr
}

func httpResponseAttributes(m httpsnoop.Metrics, w http.ResponseWriter) []attribute.KeyValue {
	var attr []attribute.KeyValue

	attr = append(attr, semconv.HTTPResponseStatusCode(m.Code))
	attr = append(attr, semconv.HTTPResponseBodySize(int(m.Written)))
	attr = append(attr, httpHeaderAttributes(w.Header(), "http.response.header")...)

	return attr
}

func httpStatusToOtelCode(status int) (codes.Code, string) {
	code := codes.Ok

	if status >= 400 {
		code = codes.Error
	}

	return code, http.StatusText(status)
}

type OpenTelemetry struct {
	serviceName string
	version     string
}

func New(serviceName, version string) *OpenTelemetry {
	return &OpenTelemetry{
		serviceName: serviceName,
		version:     version,
	}
}

// Middleware attaches logging context to the request.
func (o *OpenTelemetry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the tracing information from the HTTP headers.
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		// Add in service information.
		var attr []attribute.KeyValue

		attr = append(attr, semconv.ServiceName(o.serviceName))
		attr = append(attr, semconv.ServiceVersion(o.version))
		attr = append(attr, httpRequestAttributes(r)...)

		tracer := otel.GetTracerProvider().Tracer("opentelemetry middleware")

		// Begin the span processing.
		name := r.URL.Path

		ctx, span := tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attr...))
		defer span.End()

		// Setup logging.
		ctx = log.IntoContext(ctx, log.Log.WithValues(logValuesFromSpanContext(name, span.SpanContext())...))

		// Create a new request with any contextual information the tracer has added.
		request := r.WithContext(ctx)

		metrics := httpsnoop.CaptureMetrics(next, w, request)

		// Extract HTTP response information for logging purposes.
		span.SetAttributes(httpResponseAttributes(metrics, w)...)
		span.SetStatus(httpStatusToOtelCode(metrics.Code))
	})
}
