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

package logging_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/server/middleware/logging"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestErrorResponseLogOmitsPrincipalHeaders checks the log line of a 4xx
// response.  The middleware logs every error response, so a burst of refused
// calls must not copy the principal or a propagated client certificate into the
// log.
func TestErrorResponseLogOmitsPrincipalHeaders(t *testing.T) {
	t.Parallel()

	var lines []string

	logger := funcr.New(func(prefix, args string) {
		lines = append(lines, prefix+args)
	}, funcr.Options{})

	request := httptest.NewRequestWithContext(log.IntoContext(t.Context(), logger), http.MethodGet, "/", nil)
	request.Header.Set("X-Principal", "principal-value")
	request.Header.Set("Unikorn-Client-Certificate", "propagated-certificate")
	request.Header.Set("X-Request-Marker", "visible")

	handler := logging.New().Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request)

	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "X-Request-Marker", "the log line must show the other request headers")
	require.NotContains(t, lines[0], "X-Principal")
	require.NotContains(t, lines[0], "principal-value")
	require.NotContains(t, lines[0], "Unikorn-Client-Certificate")
	require.NotContains(t, lines[0], "propagated-certificate")
}
