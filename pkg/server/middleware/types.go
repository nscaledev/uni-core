/*
Copyright 2024-2025 the Unikorn Authors.

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

package middleware

import (
	"net/http"
)

// LoggingResponseWriter wraps a ResponseWriter in such a way that the response status code, body and headers can
// be examined after its been used to serve a response. This is deprecated: use Capture.Wrap(w) instead, because
// it wraps the ResponseWriter such that additional interfaces (e.g., io.ReaderFrom) that may be present on the
// supplied ResponseWriter are still supported in the wrapper.
type LoggingResponseWriter struct {
	http.ResponseWriter
	*Capture
}

func NewLoggingResponseWriter(next http.ResponseWriter) *LoggingResponseWriter {
	capture := NewCapture()

	return &LoggingResponseWriter{
		Capture:        capture,
		ResponseWriter: capture.Wrap(next),
	}
}
