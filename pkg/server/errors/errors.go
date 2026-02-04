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
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/trace"

	coreerrors "github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/openapi"

	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Defined by RFC7235 and RFC6750.
	AuthenticateHeader = "WWW-Authenticate"
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

	// header is a set of propagated headers.
	header http.Header

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
		description: strings.TrimSuffix(fmt.Sprintln(a...), "\n"),
		header:      http.Header{},
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

// withHeader allows headers to be sent with the error.
func (e *Error) withHeader(key, value string) *Error {
	e.header.Set(key, value)
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

	for header, values := range e.header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}

	w.WriteHeader(e.status)

	// Emit the response body.
	ge := &openapi.Error{
		Error:            e.code,
		ErrorDescription: e.description,
	}

	if id := trace.SpanContextFromContext(r.Context()).TraceID().String(); id != "" {
		ge.TraceId = ptr.To(id)
	}

	body, err := json.Marshal(ge)
	if err != nil {
		log.Error(err, "failed to marshal error response")

		return
	}

	if _, err := w.Write(body); err != nil {
		log.Error(err, "failed to write error response")

		return
	}
}

// asError is a handy unwrapper to get a HTTP error from a generic one.
func asError(err error) *Error {
	var httpErr *Error

	if !errors.As(err, &httpErr) {
		return nil
	}

	return httpErr
}

// isErrorType allows an error to be tested as an internal error type
// and also to check the HTTP error code associated with it, primarily to
// ease testing.
func isErrorType(err error, code int) bool {
	httpError := asError(err)
	if httpError == nil {
		return false
	}

	if httpError.status != code {
		return false
	}

	return true
}

// FromOpenAPIError allows propagation across API calls.
func FromOpenAPIError(code int, header http.Header, err *openapi.Error) *Error {
	return newError(code, err.Error, err.ErrorDescription)
}

// HTTPForbidden is raised when a user isn't permitted to do something by RBAC.
func HTTPForbidden(a ...any) *Error {
	return newError(http.StatusForbidden, openapi.Forbidden, a...)
}

// IsForbidden checks if the error is as described.
func IsForbidden(err error) bool {
	return isErrorType(err, http.StatusForbidden)
}

// HTTPNotFound is raised when the requested resource doesn't exist.
func HTTPNotFound() *Error {
	return newError(http.StatusNotFound, openapi.NotFound, "resource not found")
}

// IsHTTPNotFound checks if the error is as described.
func IsHTTPNotFound(err error) bool {
	return isErrorType(err, http.StatusNotFound)
}

// HTTPMethodNotAllowed is raised when the method is not supported.
func HTTPMethodNotAllowed() *Error {
	return newError(http.StatusMethodNotAllowed, openapi.MethodNotAllowed, "the requested method was not allowed")
}

// IsMethodNotAllowed checks if the error is as described.
func IsMethodNotAllowed(err error) bool {
	return isErrorType(err, http.StatusMethodNotAllowed)
}

// HTTPConflict is raised when a request conflicts with another resource.
func HTTPConflict() *Error {
	return newError(http.StatusConflict, openapi.Conflict, "the requested resource already exists")
}

// IsConflict checks if the error is as described.
func IsConflict(err error) bool {
	return isErrorType(err, http.StatusConflict)
}

// HTTPRequestEntityTooLarge is raised when the request body is too large and
// overlows internal size limits.
func HTTPRequestEntityTooLarge(a ...any) *Error {
	return newError(http.StatusRequestEntityTooLarge, openapi.RequestEntityTooLarge, a...)
}

// IsRequestEntityTooLarge checks if the error is as described.
func IsRequestEntityTooLarge(err error) bool {
	return isErrorType(err, http.StatusRequestEntityTooLarge)
}

// HTTPUnprocessableContent is used when everything is syntactically correct but
// semantically makes no sense.
func HTTPUnprocessableContent(a ...any) *Error {
	return newError(http.StatusUnprocessableEntity, openapi.UnprocessableContent, a...)
}

// IsUnprocessableContent checks if the error is as described.
func IsUnprocessableContent(err error) bool {
	return isErrorType(err, http.StatusUnprocessableEntity)
}

// OAuth2InvalidRequest indicates a client error.
func OAuth2InvalidRequest(a ...any) *Error {
	return newError(http.StatusBadRequest, openapi.InvalidRequest, a...)
}

// IsBadRequest checks if the error is as described.
func IsBadRequest(err error) bool {
	return isErrorType(err, http.StatusBadRequest)
}

// OAuth2AccessDenied tells the client the authentication failed e.g.
// username/password are wrong, or a token has expired and needs reauthentication.
func OAuth2AccessDenied(a ...any) *Error {
	return newError(http.StatusUnauthorized, openapi.AccessDenied, a...)
}

// WWWAuthenticateHeader handles encoding of WWW-Authenticate headers.
type WWWAuthenticateHeader struct {
	data map[string]map[string]string
}

// NewWWWAuthenticateHeader create a new empty WWW-Authenticate header.
func NewWWWAuthenticateHeader() *WWWAuthenticateHeader {
	return &WWWAuthenticateHeader{
		data: map[string]map[string]string{},
	}
}

// AddField adds a key value field to a chanllenge type.
func (w *WWWAuthenticateHeader) AddField(challenge, key, value string) {
	if _, ok := w.data[challenge]; !ok {
		w.data[challenge] = map[string]string{}
	}

	w.data[challenge][key] = value
}

// Encode turns the header into a string ready for the wire.  It ensures
// deterministic output.
func (w *WWWAuthenticateHeader) Encode() string {
	challenges := make([]string, len(w.data))

	challengeKeys := slices.Collect(maps.Keys(w.data))
	slices.Sort(challengeKeys)

	for i, challenge := range challengeKeys {
		fields := make([]string, len(w.data[challenge]))

		fieldKeys := slices.Collect(maps.Keys(w.data[challenge]))
		slices.Sort(fieldKeys)

		for j, key := range fieldKeys {
			fields[j] = fmt.Sprintf(`%s="%s"`, key, w.data[challenge][key])
		}

		challenges[i] = fmt.Sprintf("%s %s", challenge, strings.Join(fields, ","))
	}

	return strings.Join(challenges, ", ")
}

// AccessDenied replaces OAuth2AccessDenied.  It must be provided with the current host
// and that must implement the oidc protected resource metadata endpoint (RFC9728).
func AccessDenied(r *http.Request, a ...any) *Error {
	header := NewWWWAuthenticateHeader()
	header.AddField("Bearer", "error", string(openapi.AccessDenied))
	header.AddField("Bearer", "error_description", fmt.Sprint(a...))
	header.AddField("Bearer", "resource_metadata", "https://"+r.Host+"/.well-known/openid-protected-resource")

	return newError(http.StatusUnauthorized, openapi.AccessDenied, a...).withHeader(AuthenticateHeader, header.Encode())
}

// IsAccessDenied checks if the error is as described.
func IsAccessDenied(err error) bool {
	return isErrorType(err, http.StatusUnauthorized)
}

// PropagateError provides a response type agnostic way of extracting a human readable
// error from an API.
// NOTE: the *WithResponse APIs will have read and closed the body already and decoded
// the JSON error.  We just need to get at it, which is tricky!
func PropagateError(r *http.Response, response any) error {
	if r.StatusCode < 400 {
		return fmt.Errorf("%w: status code %d not valid", coreerrors.ErrAPIStatus, r.StatusCode)
	}

	// We expect the response to be a pointer to a struct...
	v := reflect.ValueOf(response)

	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return fmt.Errorf("%w: error response is not a struct", coreerrors.ErrTypeConversion)
	}

	// ... that through the magic of autogeneration has a field for the status code ...
	fieldName := fmt.Sprintf("JSON%d", r.StatusCode)

	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return fmt.Errorf("%w: error field %s not defined", coreerrors.ErrTypeConversion, fieldName)
	}

	if f.IsZero() {
		return fmt.Errorf("%w: error field %s not populated", coreerrors.ErrTypeConversion, fieldName)
	}

	if !f.CanInterface() {
		return fmt.Errorf("%w: error field %s not interfaceable", coreerrors.ErrTypeConversion, fieldName)
	}

	// ... which points to an Error.
	concreteError, ok := f.Interface().(*openapi.Error)
	if !ok {
		return fmt.Errorf("%w: unable to assert error", coreerrors.ErrTypeConversion)
	}

	return FromOpenAPIError(r.StatusCode, r.Header, concreteError)
}

// HandleError is the top level error handler that should be called from all
// path handlers on error.
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	if httpError := asError(err); httpError != nil {
		httpError.Write(w, r)

		return
	}

	newError(http.StatusInternalServerError, openapi.ServerError, "an internal error has occurred, please contact support").WithError(err).Write(w, r)
}
