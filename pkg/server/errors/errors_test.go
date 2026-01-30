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
	messageFixture = "this is a test"
)

var (
	errFixture = goerrors.New("fail")
)

func defaultheader() http.Header {
	h := http.Header{}
	h.Add("Content-Type", "application/json")
	h.Add("Cache-Control", "no-cache")

	return h
}

type testCase struct {
	name        string
	f           func() *errors.Error
	code        int
	validator   func(error) bool
	header      http.Header
	errorString openapi.ErrorError
	description string
}

// validate ensures codes, headers and the body is correctly populated.
func (tc *testCase) validate(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	require.Equal(t, tc.code, w.Code)
	require.Equal(t, tc.header, w.Header())

	var body openapi.Error

	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, tc.errorString, body.Error)
	require.NotEmpty(t, body.ErrorDescription)

	if tc.description != "" {
		require.Equal(t, tc.description, body.ErrorDescription)
	}
}

func request(t *testing.T) *http.Request {
	t.Helper()

	return httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://acme.corp", nil)
}

// TestDefault tests a default error is handled as a 500.
func TestDefault(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()

	errors.HandleError(w, request(t), errFixture)

	test := &testCase{
		code:        http.StatusInternalServerError,
		header:      defaultheader(),
		errorString: openapi.ServerError,
	}

	test.validate(t, w)
}

// TestNoContext tests handlers that provide no further context.
func TestNoContext(t *testing.T) {
	t.Parallel()

	tests := []testCase{
		{
			name:        "NotFound",
			f:           errors.HTTPNotFound,
			code:        http.StatusNotFound,
			header:      defaultheader(),
			validator:   errors.IsHTTPNotFound,
			errorString: openapi.NotFound,
		},
		{
			name:        "MethodNotAllowed",
			f:           errors.HTTPMethodNotAllowed,
			code:        http.StatusMethodNotAllowed,
			header:      defaultheader(),
			validator:   errors.IsMethodNotAllowed,
			errorString: openapi.MethodNotAllowed,
		},
		{
			name:        "Conflict",
			f:           errors.HTTPConflict,
			code:        http.StatusConflict,
			header:      defaultheader(),
			validator:   errors.IsConflict,
			errorString: openapi.Conflict,
		},
	}

	for i := range tests {
		test := &tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()

			errors.HandleError(w, request(t), test.f())

			test.validate(t, w)
		})
	}
}

func withContextWrapper(f func(a ...any) *errors.Error) func() *errors.Error {
	return func() *errors.Error {
		return f(messageFixture)
	}
}

// TestWithContext tests handlers that provide something useful to the end user.
func TestWithContext(t *testing.T) {
	t.Parallel()

	tests := []testCase{
		{
			name:        "Forbidden",
			f:           withContextWrapper(errors.HTTPForbidden),
			code:        http.StatusForbidden,
			header:      defaultheader(),
			validator:   errors.IsForbidden,
			errorString: openapi.Forbidden,
		},
		{
			name:        "RequestEntityTooLarge",
			f:           withContextWrapper(errors.HTTPRequestEntityTooLarge),
			code:        http.StatusRequestEntityTooLarge,
			header:      defaultheader(),
			validator:   errors.IsRequestEntityTooLarge,
			errorString: openapi.RequestEntityTooLarge,
		},
		{
			name:        "InvalidRequest",
			f:           withContextWrapper(errors.OAuth2InvalidRequest),
			code:        http.StatusBadRequest,
			header:      defaultheader(),
			validator:   errors.IsBadRequest,
			errorString: openapi.InvalidRequest,
		},
		{
			name:        "UnprocessableContent",
			f:           withContextWrapper(errors.HTTPUnprocessableContent),
			code:        http.StatusUnprocessableEntity,
			header:      defaultheader(),
			validator:   errors.IsUnprocessableContent,
			errorString: openapi.UnprocessableContent,
		},
		{
			name:        "AccessDenied",
			f:           withContextWrapper(errors.OAuth2AccessDenied),
			code:        http.StatusUnauthorized,
			header:      defaultheader(),
			validator:   errors.IsAccessDenied,
			errorString: openapi.AccessDenied,
		},
	}

	for i := range tests {
		test := &tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()

			errors.HandleError(w, request(t), test.f())

			test.validate(t, w)
		})
	}
}

type openapiResponseFixture struct {
	JSON400 *openapi.Error
}

// TestPropagateError ensures errors are correctly extracted an propagated.
func TestPropagateError(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{
		JSON400: &openapi.Error{
			Error:            openapi.InvalidRequest,
			ErrorDescription: messageFixture,
		},
	}

	err := errors.PropagateError(http.StatusBadRequest, resp)
	require.Error(t, err, "must return an error")

	require.True(t, errors.IsBadRequest(err))
}

// TestPropagateErrorUnknownCode ensures we can handle something that is unexpected
// e.g. an ingress going wrong.
func TestPropagateErrorUnknownCode(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{}

	err := errors.PropagateError(http.StatusBadGateway, resp)
	require.Error(t, err, "must return an error")

	var errorsError *errors.Error

	require.NotErrorAs(t, err, &errorsError, "must not be an API error")
}

// TestPropagateErrorUnpopulatedCode ensures we can handle something that should be
// populated but isn't e.g. faulty API error handling.
func TestPropagateErrorUnpopulatedCode(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{}

	err := errors.PropagateError(http.StatusBadRequest, resp)
	require.Error(t, err, "must return an error")

	var errorsError *errors.Error

	require.NotErrorAs(t, err, &errorsError, "must not be an API error")
}
