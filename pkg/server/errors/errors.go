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

package errors

import (
	"encoding/json"
	"errors"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// OAuth2ErrorType defines our core error type based on oauth2.
type OAuth2ErrorType string

const (
	AccessDenied            OAuth2ErrorType = "access_denied"
	Conflict                OAuth2ErrorType = "conflict"
	Forbidden               OAuth2ErrorType = "forbidden"
	InvalidClient           OAuth2ErrorType = "invalid_client"
	InvalidGrant            OAuth2ErrorType = "invalid_grant"
	InvalidRequest          OAuth2ErrorType = "invalid_request"
	InvalidScope            OAuth2ErrorType = "invalid_scope"
	MethodNotAllowed        OAuth2ErrorType = "method_not_allowed"
	NotFound                OAuth2ErrorType = "not_found"
	RequestEntityTooLarge   OAuth2ErrorType = "request_entity_too_large"
	ServerError             OAuth2ErrorType = "server_error"
	TemporarilyUnavailable  OAuth2ErrorType = "temporarily_unavailable"
	UnauthorizedClient      OAuth2ErrorType = "unauthorized_client"
	UnsupportedGrantType    OAuth2ErrorType = "unsupported_grant_type"
	UnsupportedMediaType    OAuth2ErrorType = "unsupported_media_type"
	UnsupportedResponseType OAuth2ErrorType = "unsupported_response_type"
)

// OAuth2Error is the type sent on the wire on error.
type OAuth2Error struct {
	// Error defines the error type.
	Error OAuth2ErrorType `json:"error"`
	// Description is a verbose description of the error.  This should be
	// informative to the end user, not a bunch of debugging nonsense.  We
	// keep that in telemetry dats.
	//nolint:tagliatelle
	Description string `json:"error_description"`
}

var (
	// ErrRequest is raised for all handler errors.
	ErrRequest = errors.New("request error")
)

// Error wraps ErrRequest with more contextual information that is used to
// propagate and create suitable responses.
type Error struct {
	// status is the HTTP error code.
	status int

	// code us the terse error code to return to the client.
	code OAuth2ErrorType

	// description is a verbose description to log/return to the user.
	description string

	// err is set when the originator was an error.  This is only used
	// for logging so as not to leak server internals to the client.
	err error

	// values are arbitrary key value pairs for logging.
	values []any
}

// newError returns a new HTTP error.
func newError(status int, code OAuth2ErrorType, description string) *Error {
	return &Error{
		status:      status,
		code:        code,
		description: description,
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
	return ErrRequest
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
	ge := &OAuth2Error{
		Error:       e.code,
		Description: e.description,
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

// HTTPForbidden is raised when a user isn't permitted to do something by RBAC.
func HTTPForbidden(description string) *Error {
	return newError(http.StatusForbidden, Forbidden, description)
}

// HTTPNotFound is raised when the requested resource doesn't exist.
func HTTPNotFound() *Error {
	return newError(http.StatusNotFound, NotFound, "resource not found")
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
	return newError(http.StatusMethodNotAllowed, MethodNotAllowed, "the requested method was not allowed")
}

// HTTPConflict is raised when a request conflicts with another resource.
func HTTPConflict() *Error {
	return newError(http.StatusConflict, Conflict, "the requested resource already exists")
}

func HTTPRequestEntityTooLarge(description string) *Error {
	return newError(http.StatusRequestEntityTooLarge, RequestEntityTooLarge, description)
}

// OAuth2InvalidRequest indicates a client error.
func OAuth2InvalidRequest(description string) *Error {
	return newError(http.StatusBadRequest, InvalidRequest, description)
}

// OAuth2UnauthorizedClient indicates the client is not authorized to perform the
// requested operation.
func OAuth2UnauthorizedClient(description string) *Error {
	return newError(http.StatusBadRequest, UnauthorizedClient, description)
}

// OAuth2UnsupportedGrantType is raised when the requested grant is not supported.
func OAuth2UnsupportedGrantType(description string) *Error {
	return newError(http.StatusBadRequest, UnsupportedGrantType, description)
}

// OAuth2InvalidGrant is raised when the requested grant is unknown.
func OAuth2InvalidGrant(description string) *Error {
	return newError(http.StatusBadRequest, InvalidGrant, description)
}

// OAuth2InvalidClient is raised when the client ID is not known.
func OAuth2InvalidClient(description string) *Error {
	return newError(http.StatusBadRequest, InvalidClient, description)
}

// OAuth2AccessDenied tells the client the authentication failed e.g.
// username/password are wrong, or a token has expired and needs reauthentication.
func OAuth2AccessDenied(description string) *Error {
	return newError(http.StatusUnauthorized, AccessDenied, description)
}

// OAuth2ServerError tells the client we are at fault, this should never be seen
// in production.  If so then our testing needs to improve.
func OAuth2ServerError(description string) *Error {
	return newError(http.StatusInternalServerError, ServerError, description)
}

// OAuth2InvalidScope tells the client it doesn't have the necessary scope
// to access the resource.
func OAuth2InvalidScope(description string) *Error {
	return newError(http.StatusUnauthorized, InvalidScope, description)
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
	log := log.FromContext(r.Context())

	if httpError := toError(err); httpError != nil {
		httpError.Write(w, r)

		return
	}

	log.Error(err, "unhandled error")

	OAuth2ServerError("unhandled error").Write(w, r)
}
