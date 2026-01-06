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

package errors_test

import (
	"encoding/json"
	goerrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/errors"
)

const (
	messageFixture = "Hello World!"
)

var (
	errFixture = goerrors.New("fail")
)

// validate ensures codes, headers and the body is correctly populated.
func validate(t *testing.T, w *httptest.ResponseRecorder, code int, header http.Header, errorString openapi.ErrorError, description string) {
	t.Helper()

	require.Equal(t, code, w.Code)
	require.Equal(t, header, w.Header())

	var body openapi.Error

	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, errorString, body.Error)
	require.NotEmpty(t, body.ErrorDescription)

	if description != "" {
		require.Equal(t, description, body.ErrorDescription)
	}
}

func request(t *testing.T) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://acme.corp", nil)
}

func defaultheader() http.Header {
	h := http.Header{}
	h.Add("Content-Type", "application/json")
	h.Add("Cache-Control", "no-cache")

	return h
}

// TestDefault tests a default error is handled as a 500.
func TestDefault(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()

	errors.HandleError(w, request(t), errFixture)

	validate(t, w, http.StatusInternalServerError, defaultheader(), openapi.ServerError, "")
}

// TestNoContext tests handlers that provide no further context.
func TestNoContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		f           func() *errors.Error
		code        int
		header      http.Header
		errorString openapi.ErrorError
	}{
		{
			name:        "NotFound",
			f:           errors.HTTPNotFound,
			code:        http.StatusNotFound,
			errorString: openapi.NotFound,
		},
		{
			name:        "MethodNotAllowed",
			f:           errors.HTTPMethodNotAllowed,
			code:        http.StatusMethodNotAllowed,
			errorString: openapi.MethodNotAllowed,
		},
		{
			name:        "Conflict",
			f:           errors.HTTPConflict,
			code:        http.StatusConflict,
			errorString: openapi.Conflict,
		},
	}

	for i := range tests {
		test := &tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()

			errors.HandleError(w, request(t), test.f())

			header := test.header
			if header == nil {
				header = defaultheader()
			}

			validate(t, w, test.code, header, test.errorString, "")
		})
	}
}

// TestWithContext tests handlers that provide something useful to the end user.
func TestWithContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		f           func(a ...any) *errors.Error
		code        int
		header      http.Header
		errorString openapi.ErrorError
	}{
		{
			name:        "Forbidden",
			f:           errors.HTTPForbidden,
			code:        http.StatusForbidden,
			errorString: openapi.Forbidden,
		},
		{
			name:        "RequestEntityTooLarge",
			f:           errors.HTTPRequestEntityTooLarge,
			code:        http.StatusRequestEntityTooLarge,
			errorString: openapi.RequestEntityTooLarge,
		},
		{
			name:        "InvalidRequest",
			f:           errors.OAuth2InvalidRequest,
			code:        http.StatusBadRequest,
			errorString: openapi.InvalidRequest,
		},
		{
			name:        "UnprocessableContent",
			f:           errors.HTTPUnprocessableContent,
			code:        http.StatusUnprocessableEntity,
			errorString: openapi.UnprocessableContent,
		},
		{
			name:        "AccessDenied",
			f:           errors.OAuth2AccessDenied,
			code:        http.StatusUnauthorized,
			errorString: openapi.AccessDenied,
		},
	}

	for i := range tests {
		test := &tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()

			errors.HandleError(w, request(t), test.f(messageFixture))

			header := test.header
			if header == nil {
				header = defaultheader()
			}

			validate(t, w, test.code, header, test.errorString, messageFixture)
		})
	}
}
