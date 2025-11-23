/*
Copyright 2022-2024 EscherCloud.
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

package openapi

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"
)

// Schema abstracts schema access and validation.
type Schema struct {
	// spec is the full specification.
	spec *openapi3.T

	// router is a router able to process requests and return the
	// route from the spec.
	router routers.Router
}

// SchemaGetter allows clients to get their schema from wherever.
type SchemaGetter func() (*openapi3.T, error)

// NewOpenRpi extracts the swagger document.
// NOTE: this is surprisingly slow, make sure you cache it and reuse it.
func NewSchema(get SchemaGetter) (*Schema, error) {
	spec, err := get()
	if err != nil {
		return nil, err
	}

	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		return nil, err
	}

	s := &Schema{
		spec:   spec,
		router: router,
	}

	return s, nil
}

// FindRoute looks up the route from the specification.
func (s *Schema) FindRoute(r *http.Request) (*routers.Route, map[string]string, error) {
	route, params, err := s.router.FindRoute(r)
	if err != nil {
		err = fmt.Errorf("failed to find route: %w", err)
		return nil, nil, err
	}

	return route, params, nil
}

func parseJSONErrorResponse(headers http.Header, data []byte) error {
	var response errorsv2.Error
	if err := json.Unmarshal(data, &response); err != nil {
		return errorsv2.NewInternalError().WithCause(err).Prefixed()
	}

	return response.WithSimpleCause("upstream error").
		WithWWWAuthenticate(headers).
		WithOAuth2ErrorCode(headers).
		WithAPIErrorCode(headers)
}

//nolint:nlreturn,wsl
func ParseJSONValueResponse[T any](headers http.Header, data []byte, status, expected int) (T, error) {
	if status != expected {
		var zero T
		err := parseJSONErrorResponse(headers, data)
		return zero, err
	}

	var response T
	if err := json.Unmarshal(data, &response); err != nil {
		err = errorsv2.NewInternalError().WithCause(err).Prefixed()
		return response, err
	}

	return response, nil
}

//nolint:nlreturn,wsl
func ParseJSONPointerResponse[T any](headers http.Header, data []byte, status, expected int) (*T, error) {
	if status != expected {
		var zero T
		err := parseJSONErrorResponse(headers, data)
		return &zero, err
	}

	var response T
	if err := json.Unmarshal(data, &response); err != nil {
		err = errorsv2.NewInternalError().WithCause(err).Prefixed()
		return nil, err
	}

	return &response, nil
}

//nolint:nlreturn,wsl
func AssertResponseStatus(headers http.Header, status, expected int) error {
	if status != expected {
		return parseJSONErrorResponse(headers, nil)
	}
	return nil
}
