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

package routeresolver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/getkin/kin-openapi/routers"

	"github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/openapi/helpers"
	servererrors "github.com/unikorn-cloud/core/pkg/server/errors"
)

type RouteInfo struct {
	Route      *routers.Route
	Parameters map[string]string
}

type RouteInfoKeyType int

const (
	RouteInfoKey RouteInfoKeyType = iota
)

func FromContext(ctx context.Context) (*RouteInfo, error) {
	v, ok := ctx.Value(RouteInfoKey).(*RouteInfo)
	if !ok {
		return nil, fmt.Errorf("%w: route info not in context", errors.ErrKey)
	}

	return v, nil
}

// RouteResolver performs the relatively costly translation from request URL to an
// OpenAPI route once and stashes it in the context for easy use by other middlewares.
type RouteResolver struct {
	schema *helpers.Schema
}

func New(schema *helpers.Schema) *RouteResolver {
	return &RouteResolver{
		schema: schema,
	}
}

func (m *RouteResolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeRequest := r

		// CORS requests are special in that they are emulated, so we
		// need to resolve the correct method for the route before passing
		// it to the CORS middleware for handling.
		if r.Method == http.MethodOptions {
			method := r.Header.Get("Access-Control-Request-Method")
			if method == "" {
				servererrors.HandleError(w, r, servererrors.OAuth2InvalidRequest("OPTIONS missing Access-Control-Request-Method header"))
				return
			}

			routeRequest = r.Clone(r.Context())
			routeRequest.Method = method
		}

		route, parameters, err := m.schema.FindRoute(routeRequest)
		if err != nil {
			servererrors.HandleError(w, r, err)
			return
		}

		ctx := context.WithValue(r.Context(), RouteInfoKey, &RouteInfo{
			Route:      route,
			Parameters: parameters,
		})

		request := r.Clone(ctx)

		next.ServeHTTP(w, request)
	})
}
