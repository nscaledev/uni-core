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

package api

import (
	goerrors "errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/errors"
)

var (
	ErrExtraction = goerrors.New("api error extraction error")
)

// ExtractError provides a response type agnostic way of extracting a human readable
// error from an API.
func ExtractError(statusCode int, response any) error {
	if statusCode < 400 {
		return fmt.Errorf("%w: status code %d not valid", ErrExtraction, statusCode)
	}

	// We expect the response to be a pointer to a struct...
	v := reflect.ValueOf(response)

	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return fmt.Errorf("%w: error response is not a struct", ErrExtraction)
	}

	// ... that through the magic of autogeneration has a field for the status code ...
	fieldName := fmt.Sprintf("JSON%d", statusCode)

	f := v.FieldByName(fieldName)
	if !f.IsValid() {
		return fmt.Errorf("%w: error field %s not defined", ErrExtraction, fieldName)
	}

	if f.IsZero() {
		return fmt.Errorf("%w: error field %s not populated", ErrExtraction, fieldName)
	}

	if !f.CanInterface() {
		return fmt.Errorf("%w: error field %s not interfaceable", ErrExtraction, fieldName)
	}

	// ... which points to an Error.
	concreteError, ok := f.Interface().(*openapi.Error)
	if !ok {
		return fmt.Errorf("%w: unable to assert error", ErrExtraction)
	}

	return errors.FromOpenAPIError(statusCode, http.Header{}, concreteError)
}
