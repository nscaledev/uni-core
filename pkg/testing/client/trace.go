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

package client

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// generateTraceID creates a new W3C trace ID.
// We are using this to create a new trace ID for each request so if an error occurs we can find the request in the logs.
func generateTraceID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)

	return hex.EncodeToString(bytes)
}

// generateSpanID creates a new W3C span ID.
func generateSpanID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)

	return hex.EncodeToString(bytes)
}

// CreateTraceParent creates a W3C traceparent header value.
func CreateTraceParent() string {
	traceID := generateTraceID()
	spanID := generateSpanID()

	return fmt.Sprintf("00-%s-%s-01", traceID, spanID)
}

// ExtractTraceID extracts the trace ID from a traceparent header value.
func ExtractTraceID(traceParent string) string {
	parts := strings.Split(traceParent, "-")
	if len(parts) >= 2 {
		return parts[1]
	}

	return traceParent
}

// ExtractSpanID extracts the span ID from a traceparent header value.
func ExtractSpanID(traceParent string) string {
	parts := strings.Split(traceParent, "-")
	if len(parts) >= 3 {
		return parts[2]
	}

	return ""
}

// FormatTraceContext formats trace ID and span ID for log output.
func FormatTraceContext(traceParent string) string {
	traceID := ExtractTraceID(traceParent)
	spanID := ExtractSpanID(traceParent)

	return fmt.Sprintf("traceID=%s spanID=%s", traceID, spanID)
}
