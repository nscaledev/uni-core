/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.
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

package helpers

import (
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	chi "github.com/go-chi/chi/v5"

	"github.com/unikorn-cloud/core/pkg/server/errors"
)

// Schema abstracts schema access and validation.
type Schema struct {
	// spec is the full specification.
	spec *openapi3.T
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

	s := &Schema{
		spec: spec,
	}

	return s, nil
}

// FindRoute looks up the route from the specification.
func (s *Schema) FindRoute(r *http.Request) (*routers.Route, map[string]string, error) {
	rctx := chi.RouteContext(r.Context())

	routePath := rctx.Routes.Find(rctx, r.Method, r.URL.Path)
	if routePath == "" {
		return nil, nil, errors.HTTPNotFound().WithValues("path", r.URL.String())
	}

	path := s.spec.Paths.Find(routePath)
	if path == nil {
		return nil, nil, errors.HTTPNotFound().WithValues("path", r.URL.String())
	}

	operation := path.GetOperation(r.Method)
	if operation == nil {
		return nil, nil, errors.HTTPMethodNotAllowed().WithValues("path", r.URL.String(), "method", r.Method)
	}

	route := &routers.Route{
		Spec:      s.spec,
		Path:      routePath,
		PathItem:  path,
		Method:    r.Method,
		Operation: operation,
	}

	parameters := map[string]string{}

	for i := range rctx.URLParams.Keys {
		parameters[rctx.URLParams.Keys[i]] = rctx.URLParams.Values[i]
	}

	return route, parameters, nil
}
