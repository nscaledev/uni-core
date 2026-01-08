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

package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/unikorn-cloud/core/pkg/openapi"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Error wraps ErrRequest with more contextual information that is used to
// propagate and create suitable responses.
type Error struct {
	// status is the HTTP error code.
	status int

	// code us the terse error code to return to the client.
	code openapi.ErrorError

	// description is a verbose description to log/return to the user.
	description string

	// err is set when the originator was an error.  This is only used
	// for logging so as not to leak server internals to the client.
	err error

	// values are arbitrary key value pairs for logging.
	values []any
}

// newError returns a new HTTP error.
func newError(status int, code openapi.ErrorError, a ...any) *Error {
	return &Error{
		status:      status,
		code:        code,
		description: fmt.Sprint(a...),
	}
}

// WithError augments the error with an error from a library.
func (e *Error) WithError(err error) *Error {
	e.err = err

	return e
}

// WithValues augments the error with a set of K/V pairs.
// Values should not use the "error" key as that's implicitly defined
// by WithError and could collide.
func (e *Error) WithValues(values ...any) *Error {
	e.values = values

	return e
}

// Unwrap implements Go 1.13 errors.
func (e *Error) Unwrap() error {
	return e.err
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.description
}

// Write returns the error code and description to the client.
func (e *Error) Write(w http.ResponseWriter, r *http.Request) {
	// Log out any detail from the error that shouldn't be
	// reported to the client.  Do it before things can error
	// and return.
	log := log.FromContext(r.Context())

	var details []any

	if e.description != "" {
		details = append(details, "detail", e.description)
	}

	if e.err != nil {
		details = append(details, "error", e.err)
	}

	if e.values != nil {
		details = append(details, e.values...)
	}

	log.Info("error detail", details...)

	// Emit the response to the client.
	w.Header().Add("Cache-Control", "no-cache")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(e.status)

	// Emit the response body.
	ge := &openapi.Error{
		Error:            e.code,
		ErrorDescription: e.description,
	}

	body, err := json.Marshal(ge)
	if err != nil {
		log.Error(err, "failed to marshal error response")

		return
	}

	if _, err := w.Write(body); err != nil {
		log.Error(err, "failed to wirte error response")

		return
	}
}

// FromOpenAPIError allows propagation across API calls.
func FromOpenAPIError(code int, err *openapi.Error) *Error {
	return newError(code, err.Error, err.ErrorDescription)
}

// HTTPForbidden is raised when a user isn't permitted to do something by RBAC.
func HTTPForbidden(a ...any) *Error {
	return newError(http.StatusForbidden, openapi.Forbidden, a...)
}

// HTTPNotFound is raised when the requested resource doesn't exist.
func HTTPNotFound() *Error {
	return newError(http.StatusNotFound, openapi.NotFound, "resource not found")
}

// IsHTTPNotFound interrogates the error type.
func IsHTTPNotFound(err error) bool {
	httpError := &Error{}

	if ok := errors.As(err, &httpError); !ok {
		return false
	}

	if httpError.status != http.StatusNotFound {
		return false
	}

	return true
}

// HTTPMethodNotAllowed is raised when the method is not supported.
func HTTPMethodNotAllowed() *Error {
	return newError(http.StatusMethodNotAllowed, openapi.MethodNotAllowed, "the requested method was not allowed")
}

// HTTPConflict is raised when a request conflicts with another resource.
func HTTPConflict() *Error {
	return newError(http.StatusConflict, openapi.Conflict, "the requested resource already exists")
}

func HTTPRequestEntityTooLarge(a ...any) *Error {
	return newError(http.StatusRequestEntityTooLarge, openapi.RequestEntityTooLarge, a...)
}

func HTTPUnprocessableContent(a ...any) *Error {
	return newError(http.StatusUnprocessableEntity, openapi.UnprocessableContent, a...)
}

// OAuth2InvalidRequest indicates a client error.
func OAuth2InvalidRequest(a ...any) *Error {
	return newError(http.StatusBadRequest, openapi.InvalidRequest, a...)
}

// OAuth2AccessDenied tells the client the authentication failed e.g.
// username/password are wrong, or a token has expired and needs reauthentication.
func OAuth2AccessDenied(a ...any) *Error {
	return newError(http.StatusUnauthorized, openapi.AccessDenied, a...)
}

// OAuth2ServerError tells the client we are at fault, this should never be seen
// in production.  If so then our testing needs to improve.
// Deprecated: this should be deleted everywhere and implicit handling used for brevity.
func OAuth2ServerError(a ...any) *Error {
	return newError(http.StatusInternalServerError, openapi.ServerError, a...)
}

// toError is a handy unwrapper to get a HTTP error from a generic one.
func toError(err error) *Error {
	var httpErr *Error

	if !errors.As(err, &httpErr) {
		return nil
	}

	return httpErr
}

// HandleError is the top level error handler that should be called from all
// path handlers on error.
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	if httpError := toError(err); httpError != nil {
		httpError.Write(w, r)

		return
	}

	OAuth2ServerError("an internal error has occurred, please contact support").WithError(err).Write(w, r)
}
