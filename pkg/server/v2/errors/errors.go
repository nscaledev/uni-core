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

//nolint:err113,nlreturn,wsl
package errorsv2

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	HeaderWWWAuthenticate = "WWW-Authenticate"
	HeaderOAuth2Error     = "X-Oauth2-Error"
	HeaderAPIError        = "X-Api-Error"
)

type ErrorType string

const (
	ErrorTypeOAuth2Error ErrorType = "oauth2_error"
	ErrorTypeAPIError    ErrorType = "api_error"
)

type OAuth2ErrorCode string

const (
	OAuth2ErrorCodeInvalidRequest       OAuth2ErrorCode = "invalid_request"
	OAuth2ErrorCodeUnsupportedGrantType OAuth2ErrorCode = "unsupported_grant_type"
	OAuth2ErrorCodeInvalidGrant         OAuth2ErrorCode = "invalid_grant"
	OAuth2ErrorCodeInvalidToken         OAuth2ErrorCode = "invalid_token"
	OAuth2ErrorCodeInvalidClient        OAuth2ErrorCode = "invalid_client"
	OAuth2ErrorCodeAccessDenied         OAuth2ErrorCode = "access_denied"
	OAuth2ErrorCodeInsufficientScope    OAuth2ErrorCode = "insufficient_scope"
	OAuth2ErrorCodeServerError          OAuth2ErrorCode = "server_error"
)

type APIErrorCode string

const (
	APIErrorCodeInvalidRequest   APIErrorCode = "invalid_request"
	APIErrorCodeTokenExpired     APIErrorCode = "token_expired"
	APIErrorCodeUnauthorized     APIErrorCode = "unauthorized"
	APIErrorCodeResourceMissing  APIErrorCode = "resource_missing"
	APIErrorCodeMethodNotAllowed APIErrorCode = "method_not_allowed"
	APIErrorCodeConflict         APIErrorCode = "conflict"
	APIErrorCodeQuotaExhausted   APIErrorCode = "quota_exhausted"
	APIErrorCodeInternal         APIErrorCode = "internal"
)

type SimpleError struct {
	Message string `json:"message"`
}

func NewSimpleError(message string) *SimpleError {
	return &SimpleError{
		Message: message,
	}
}

func NewSimpleErrorf(format string, a ...any) *SimpleError {
	return &SimpleError{
		Message: fmt.Sprintf(format, a...),
	}
}

func (e *SimpleError) Error() string {
	return e.Message
}

// Error represents an error response for both API and OAuth, preserving backward compatibility.
//
//nolint:tagliatelle
type Error struct {
	Prefix string `json:"-"`
	Cause  error  `json:"-"`

	WWWAuthenticate string          `json:"-"`
	OAuth2ErrorCode OAuth2ErrorCode `json:"-"`
	APIErrorCode    APIErrorCode    `json:"-"`

	Type             ErrorType `json:"type"`
	Status           int       `json:"status"`
	TraceID          string    `json:"trace_id"`
	ErrorCode        string    `json:"error"`
	ErrorDescription string    `json:"error_description"`
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}

	if e.Prefix != "" {
		return e.Prefix
	}

	return "error occurred with no additional information"
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) WithSimpleCause(message string) *Error {
	e.Cause = NewSimpleError(message)
	return e
}

func (e *Error) WithSimpleCausef(format string, a ...any) *Error {
	e.Cause = NewSimpleErrorf(format, a...)
	return e
}

func (e *Error) WithCause(cause error) *Error {
	e.Cause = cause
	return e
}

func (e *Error) WithCausef(format string, a ...any) *Error {
	e.Cause = fmt.Errorf(format, a...)
	return e
}

func (e *Error) WithWWWAuthenticate(headers http.Header) *Error {
	e.WWWAuthenticate = headers.Get(HeaderWWWAuthenticate)
	return e
}

func (e *Error) WithOAuth2ErrorCode(headers http.Header) *Error {
	code := headers.Get(HeaderOAuth2Error)
	e.OAuth2ErrorCode = OAuth2ErrorCode(code)
	return e
}

func (e *Error) WithAPIErrorCode(headers http.Header) *Error {
	code := headers.Get(HeaderAPIError)
	e.APIErrorCode = APIErrorCode(code)
	return e
}

func (e *Error) WithErrorDescription(description string) *Error {
	e.ErrorDescription = description
	return e
}

func (e *Error) WithErrorDescriptionf(format string, a ...any) *Error {
	e.ErrorDescription = fmt.Sprintf(format, a...)
	return e
}

// Prefixed adds the given Prefix to the underlying Cause of an error.
// It should only be called at the end of the error construction chain.
func (e *Error) Prefixed() *Error {
	if e.Prefix == "" {
		return e
	}

	if e.Cause == nil {
		e.Cause = fmt.Errorf("%s: unknown cause", e.Prefix)
	} else {
		e.Cause = fmt.Errorf("%s: %w", e.Prefix, e.Cause)
	}

	return e
}

func NewInvalidRequestError() *Error {
	return &Error{
		Prefix:          "invalid request",
		OAuth2ErrorCode: OAuth2ErrorCodeInvalidRequest,
		APIErrorCode:    APIErrorCodeInvalidRequest,
		Status:          http.StatusBadRequest,
	}
}

func NewUnsupportedGrantTypeError() *Error {
	return &Error{
		Prefix:          "unsupported grant type",
		OAuth2ErrorCode: OAuth2ErrorCodeUnsupportedGrantType,
		APIErrorCode:    APIErrorCodeInvalidRequest,
		Status:          http.StatusBadRequest,
	}
}

func NewInvalidGrantError() *Error {
	return &Error{
		Prefix:          "invalid grant",
		OAuth2ErrorCode: OAuth2ErrorCodeInvalidGrant,
		APIErrorCode:    APIErrorCodeInvalidRequest,
		Status:          http.StatusBadRequest,
	}
}

func NewInvalidTokenError() *Error {
	return &Error{
		Prefix:           "invalid token",
		OAuth2ErrorCode:  OAuth2ErrorCodeInvalidToken,
		APIErrorCode:     APIErrorCodeUnauthorized,
		Status:           http.StatusUnauthorized,
		ErrorDescription: "The provided token is expired, revoked, malformed, or otherwise invalid.",
	}
}

func NewInvalidClientError() *Error {
	return &Error{
		Prefix:          "invalid client",
		OAuth2ErrorCode: OAuth2ErrorCodeInvalidClient,
		APIErrorCode:    APIErrorCodeUnauthorized,
		Status:          http.StatusUnauthorized,
	}
}

func NewTokenExpiredError() *Error {
	return &Error{
		Cause:            NewSimpleError("token is expired"),
		OAuth2ErrorCode:  OAuth2ErrorCodeInvalidToken,
		APIErrorCode:     APIErrorCodeTokenExpired,
		Status:           http.StatusUnauthorized,
		ErrorDescription: "The provided token has expired.",
	}
}

func NewAccessDeniedError() *Error {
	return &Error{
		Prefix:          "access denied",
		OAuth2ErrorCode: OAuth2ErrorCodeAccessDenied,
		APIErrorCode:    APIErrorCodeUnauthorized,
		Status:          http.StatusForbidden,
	}
}

func NewInsufficientScopeError() *Error {
	return &Error{
		Prefix:           "insufficient scope",
		OAuth2ErrorCode:  OAuth2ErrorCodeInsufficientScope,
		APIErrorCode:     APIErrorCodeUnauthorized,
		Status:           http.StatusForbidden,
		ErrorDescription: "The operation is not allowed with the provided token scope.",
	}
}

func NewResourceMissingError(resource string) *Error {
	return &Error{
		Prefix:           fmt.Sprintf("%s not found", resource),
		APIErrorCode:     APIErrorCodeResourceMissing,
		Status:           http.StatusNotFound,
		ErrorDescription: "The requested resource was not found.",
	}
}

func NewMethodNotAllowedError() *Error {
	return &Error{
		Cause:            NewSimpleError("method not allowed"),
		APIErrorCode:     APIErrorCodeMethodNotAllowed,
		Status:           http.StatusMethodNotAllowed,
		ErrorDescription: "The requested method is not allowed for the specified resource.",
	}
}

func NewConflictError() *Error {
	return &Error{
		Prefix:       "conflict",
		APIErrorCode: APIErrorCodeConflict,
		Status:       http.StatusConflict,
	}
}

func NewQuotaExhaustedError(resource string, desired, limit int64) *Error {
	return &Error{
		Prefix:           fmt.Sprintf("quota exhausted for resource %s", resource),
		APIErrorCode:     APIErrorCodeQuotaExhausted,
		Status:           http.StatusConflict,
		ErrorDescription: fmt.Sprintf("The requested allocation of %d for %s exceeds the allowed limit of %d.", desired, resource, limit),
	}
}

func NewInternalError() *Error {
	return &Error{
		Prefix:           "internal",
		OAuth2ErrorCode:  OAuth2ErrorCodeServerError,
		APIErrorCode:     APIErrorCodeInternal,
		Status:           http.StatusInternalServerError,
		ErrorDescription: "The server encountered an unexpected condition. Please try again later. If the problem persists, please contact our team.",
	}
}

func AsOAuth2Error(err error) (*Error, bool) {
	if e := (*Error)(nil); errors.As(err, &e) && e.OAuth2ErrorCode != "" {
		return e, true
	}
	return nil, false
}

func AsAPIError(err error) (*Error, bool) {
	if e := (*Error)(nil); errors.As(err, &e) && e.APIErrorCode != "" {
		return e, true
	}
	return nil, false
}

func IsAPIResourceMissingError(err error) bool {
	e, ok := AsAPIError(err)
	return ok && e.APIErrorCode == APIErrorCodeResourceMissing
}
