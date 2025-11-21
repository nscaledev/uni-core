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

package handler

import (
	"net/http"

	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"
	"github.com/unikorn-cloud/core/pkg/server/v2/httputil"
)

// NotFound is called from the router when a path is not found.
func NotFound(w http.ResponseWriter, r *http.Request) {
	err := errorsv2.NewResourceMissingError("API").Prefixed()
	httputil.WriteErrorResponse(w, r, err)
}

// MethodNotAllowed is called from the router when a method is not found for a path.
func MethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	err := errorsv2.NewMethodNotAllowedError().Prefixed()
	httputil.WriteErrorResponse(w, r, err)
}

// HandleError is called when the router has trouble parsing paths.
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	err = errorsv2.NewInvalidRequestError().
		WithCause(err).
		WithErrorDescription("The request is missing required parameters or is incorrectly formatted.").
		Prefixed()

	httputil.WriteErrorResponse(w, r, err)
}
