/*
Copyright 2025 the Unikorn Authors.

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

package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/unikorn-cloud/core/pkg/server/middleware"
)

func TestLoggingResponseWriter(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	lw := middleware.NewLoggingResponseWriter(w)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lw.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(lw, "foo")
		_, _ = io.WriteString(lw, "bar")
	})
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(lw, r)

	assert.Equal(t, lw.StatusCode(), http.StatusAccepted)
	assert.Equal(t, len("foo"+"bar"), lw.Body().Len())
}
