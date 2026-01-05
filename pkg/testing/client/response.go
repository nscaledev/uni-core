/*
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

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrResourceNotFound indicates a resource was not found.
	ErrResourceNotFound = errors.New("resource not found")
	// ErrAccessDenied indicates access to a resource was denied.
	ErrAccessDenied = errors.New("access denied")
	// ErrServerError indicates a server error occurred.
	ErrServerError = errors.New("server error")
	// ErrUnexpectedStatus indicates an unexpected status code was received.
	ErrUnexpectedStatus = errors.New("unexpected status code")
)

// ResponseHandlerConfig configures how different status codes should be handled.
type ResponseHandlerConfig struct {
	ResourceType   string
	ResourceID     string
	ResourceIDType string
	AllowForbidden bool
	AllowNotFound  bool
}

// HandleResourceListResponse handles common response patterns for resource listing endpoints using type-safe generics.
// Returns an empty slice with an error for error cases where AllowForbidden or AllowNotFound is true.
// Type parameter T should be the element type (e.g., openapi.Cluster, openapi.Region).
func HandleResourceListResponse[T any](resp *http.Response, respBody []byte, config ResponseHandlerConfig) ([]T, error) {
	var zero []T

	switch resp.StatusCode {
	case http.StatusOK:
		var resources []T
		if err := json.Unmarshal(respBody, &resources); err != nil {
			return zero, fmt.Errorf("unmarshaling %s response: %w", config.ResourceType, err)
		}

		return resources, nil
	case http.StatusNotFound:
		if config.AllowNotFound {
			return zero, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrResourceNotFound)
		}

		return zero, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrResourceNotFound)
	case http.StatusForbidden:
		if config.AllowForbidden {
			return zero, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrAccessDenied)
		}

		return zero, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrAccessDenied)
	case http.StatusInternalServerError:
		return zero, fmt.Errorf("reading %s for %s '%s' (status: %d): %s: %w", config.ResourceType, config.ResourceIDType, config.ResourceID, resp.StatusCode, string(respBody), ErrServerError)
	default:
		return zero, fmt.Errorf("status code %d: %w", resp.StatusCode, ErrUnexpectedStatus)
	}
}
