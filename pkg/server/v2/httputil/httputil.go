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

//nolint:wsl
package httputil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/trace"

	errorsv2 "github.com/unikorn-cloud/core/pkg/server/v2/errors"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func ReadJSONRequestBody[T any](reader io.Reader) (*T, error) {
	var body T

	if err := json.NewDecoder(reader).Decode(&body); err != nil {
		err = errorsv2.NewInvalidRequestError().
			WithCausef("failed to unmarshal request body: %w", err).
			WithErrorDescription("The request body is malformed or invalid. Please verify that it is properly formatted.").
			Prefixed()

		return nil, err
	}

	return &body, nil
}

func WriteJSONResponse(w http.ResponseWriter, r *http.Request, code int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.FromContext(r.Context()).Error(err, "failed to write response")
	}
}

func WriteErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	if strings.HasPrefix(r.URL.Path, "/oauth2/") {
		WriteOAuth2ErrorResponse(w, r, err)
		return
	}
	WriteAPIErrorResponse(w, r, err)
}

func writeErrorCodeHeaders(w http.ResponseWriter, e *errorsv2.Error) {
	w.Header().Set(errorsv2.HeaderOAuth2Error, string(e.OAuth2ErrorCode))
	w.Header().Set(errorsv2.HeaderAPIError, string(e.APIErrorCode))
}

func WriteOAuth2ErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	var (
		ctx  = r.Context()
		ulog = log.FromContext(ctx)
	)

	response, ok := errorsv2.AsOAuth2Error(err)
	if ok {
		ulog.Info("error detail", "error", err)
	} else {
		message := fmt.Sprintf("caught unhandled oauth2 error of type %s", reflect.TypeOf(err))
		ulog.Error(err, message)

		response = errorsv2.NewInternalError().WithCause(err).Prefixed()
	}

	response.Type = errorsv2.ErrorTypeOAuth2Error
	response.TraceID = trace.SpanContextFromContext(ctx).TraceID().String()
	response.ErrorCode = string(response.OAuth2ErrorCode)

	writeErrorCodeHeaders(w, response)

	WriteJSONResponse(w, r, response.Status, response)
}

func writeWWWAuthenticateHeader(w http.ResponseWriter, e *errorsv2.Error) {
	if e.WWWAuthenticate != "" {
		w.Header().Set(errorsv2.HeaderWWWAuthenticate, e.WWWAuthenticate)
		return
	}

	var builder strings.Builder

	builder.WriteString("Bearer error=")
	builder.WriteString(strconv.Quote(string(e.OAuth2ErrorCode)))

	if e.ErrorDescription != "" {
		builder.WriteString(", error_description=")
		builder.WriteString(strconv.Quote(e.ErrorDescription))
	}

	w.Header().Set(errorsv2.HeaderWWWAuthenticate, builder.String())
}

func WriteAPIErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	var (
		ctx  = r.Context()
		ulog = log.FromContext(ctx)
	)

	response, ok := errorsv2.AsAPIError(err)
	if ok {
		ulog.Info("error detail", "error", err)
	} else {
		message := fmt.Sprintf("caught unhandled API error of type %s", reflect.TypeOf(err))
		ulog.Error(err, message)

		response = errorsv2.NewInternalError().WithCause(err).Prefixed()
	}

	response.Type = errorsv2.ErrorTypeAPIError
	response.TraceID = trace.SpanContextFromContext(ctx).TraceID().String()
	response.ErrorCode = string(response.APIErrorCode)

	writeWWWAuthenticateHeader(w, response)
	writeErrorCodeHeaders(w, response)

	WriteJSONResponse(w, r, response.Status, response)
}
