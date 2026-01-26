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

package logging

import (
	"net/http"

	"github.com/felixge/httpsnoop"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Middleware is an object that performs logging.
// NOTE: while it appears to do nothing, it does allow easy addition of
// configuration, works the same as everything else, ensuring we don't
// alloate it all the time, and it actually shows up in pprof traces
// rather than some anonymous closure.
type Middleware struct {
}

// New creates a new logging middleware.
func New() *Middleware {
	return &Middleware{}
}

// headers processes HTTP headers and removes any that are commonly considers
// sensitive information about authentication, authorization, and anything that
// could be used to identify a user or organization that may also be subject
// to GDPR etc.
func headers(h http.Header) http.Header {
	if len(h) == 0 {
		return nil
	}

	blacklist := []string{
		"Authorization",
		"Cookie",
		"User-Agent",
		"Set-Cookie",
		"X-Forwarded-For",
	}

	headers := h.Clone()

	for _, i := range blacklist {
		headers.Del(i)
	}

	return headers
}

// RequestLog wraps up the request log formatting so it's printed in a
// deterministic and sane order.
type RequestLog struct {
	// Protocol is the HTTP protocol e.g. HTTP/2.
	Protocol string `json:"protocol,omitempty"`
	// Scheme is the HTTP scheme in use e.g. https.
	Scheme string `json:"scheme,omitempty"`
	// Method is the HTTP method e.g. GET
	Method string `json:"method,omitempty"`
	// Path is the HTTP URL's path.
	Path string `json:"path,omitempty"`
	// Host is the host that was requested as defined by HTTP/1.1.
	Host string `json:"host,omitempty"`
	// Query is the HTTP query string.
	Query string `json:"query,omitempty"`
	// Fragment is the HTTP fragment string.
	Fragment string `json:"fragment,omitempty"`
	// Length is the request's content length.
	Length int64 `json:"length,omitempty"`
	// Address is the address that made the connection.
	// NOTE: this should always be an API gateway of some variety.
	Address string `json:"address,omitempty"`
	// Headers is the set of HTTP headers requested by the client.
	Headers http.Header `json:"headers,omitempty"`
}

// request creates a log request object from a HTTP request.
func request(r *http.Request) *RequestLog {
	return &RequestLog{
		Protocol: r.Proto,
		Scheme:   r.URL.Scheme,
		Method:   r.Method,
		Path:     r.URL.Path,
		Host:     r.URL.Host,
		Query:    r.URL.RawQuery,
		Fragment: r.URL.Fragment,
		Length:   r.ContentLength,
		Address:  r.RemoteAddr,
		Headers:  headers(r.Header),
	}
}

// ResponseLog wraps up the response log formatting so it's printed in a
// deterministic and sane order.
type ResponseLog struct {
	// Code is the HTTP status code.
	Code int `json:"code"`
	// Length is the response's content length.
	Length int64 `json:"length"`
	// TimeNS records the  time in nanoseconds the request took to be processed.
	TimeNS int64 `json:"timeNs"`
	// Headers is the set of HTTP headers send by the server.
	Headers http.Header `json:"headers,omitempty"`
}

// response creates a log response object from a HTTP response and any metrics
// collected about it.
func response(w http.ResponseWriter, metrics httpsnoop.Metrics) *ResponseLog {
	return &ResponseLog{
		Code:    metrics.Code,
		Length:  metrics.Written,
		TimeNS:  metrics.Duration.Nanoseconds(),
		Headers: headers(w.Header()),
	}
}

// logRequest logs the request to the console.  In general this is unnecessary as
// all the data is also captured in the response, and as such is disabled by
// default to reduce log noise and improve performance.
func (m *Middleware) logRequest(r *http.Request) {
	log := log.FromContext(r.Context())

	if !log.V(1).Enabled() {
		return
	}

	log.Info("http request", "request", request(r))
}

// logResponse logs the response to the console.  Like requests, most good responses
// are unnecessary by default.  What is always useful is when an error occurs so we
// capture any 4XX and 5XX errors unconditionally.
func (m *Middleware) logResponse(r *http.Request, w http.ResponseWriter, metrics httpsnoop.Metrics) {
	log := log.FromContext(r.Context())

	if !log.V(1).Enabled() || metrics.Code < 400 {
		return
	}

	log.Info("http response", "request", request(r), "response", response(w, metrics))
}

// Middleware provides an adaptor into chi's routing stack.
func (m *Middleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.logRequest(r)

		metrics := httpsnoop.CaptureMetrics(next, w, r)

		m.logResponse(r, w, metrics)
	})
}
