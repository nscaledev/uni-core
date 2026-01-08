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

package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/util/api"
)

const (
	description = "this is a test"
)

type openapiResponseFixture struct {
	JSON400 *openapi.Error
}

// TestExtractError ensures errors are correctly extracted an propagated.
func TestExtractError(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{
		JSON400: &openapi.Error{
			Error:            openapi.InvalidRequest,
			ErrorDescription: description,
		},
	}

	err := api.ExtractError(http.StatusBadRequest, resp)
	require.Error(t, err, "must return an error")

	var apiError *errors.Error

	require.ErrorAs(t, err, &apiError, "must be an API error")
}

// TestExtractErrorUnknownCode ensures we can handle something that is unexpected
// e.g. an ingress going wrong.
func TestExtractErrorUnknownCode(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{}

	err := api.ExtractError(http.StatusBadGateway, resp)
	require.Error(t, err, "must return an error")

	var apiError *errors.Error

	require.NotErrorAs(t, err, &apiError, "must not be an API error")
}

// TestExtractErrorUnpopulatedCode ensures we can handle something that should be
// populated but isn't e.g. faulty API error handling.
func TestExtractErrorUnpopulatedCode(t *testing.T) {
	t.Parallel()

	resp := &openapiResponseFixture{}

	err := api.ExtractError(http.StatusBadRequest, resp)
	require.Error(t, err, "must return an error")

	var apiError *errors.Error

	require.NotErrorAs(t, err, &apiError, "must not be an API error")
}
