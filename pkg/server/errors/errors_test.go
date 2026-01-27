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
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/errors"
)

const (
	description = "this is a test"
)

type openapiResponseFixture struct {
	JSON400 *openapi.Error
}

// TestPropagateError ensures errors are correctly extracted an propagated.
func TestPropagateError(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{
		JSON400: &openapi.Error{
			Error:            openapi.InvalidRequest,
			ErrorDescription: description,
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
