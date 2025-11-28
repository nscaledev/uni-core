/*
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

// HandleResourceListResponse handles common response patterns for resource listing endpoints.
// Returns an empty slice with an error for error cases where AllowForbidden or AllowNotFound is true.
//
// NOTE: This function uses map[string]interface{} for cross-service responses where typed structs
// are not available (e.g., Region service responses proxied through Compute service).
// For service-owned responses, prefer using typed response handlers.
func HandleResourceListResponse(resp *http.Response, respBody []byte, config ResponseHandlerConfig) ([]map[string]interface{}, error) {
	switch resp.StatusCode {
	case http.StatusOK:
		var resources []map[string]interface{}
		if err := json.Unmarshal(respBody, &resources); err != nil {
			return nil, fmt.Errorf("unmarshaling %s response: %w", config.ResourceType, err)
		}

		return resources, nil
	case http.StatusNotFound:
		if config.AllowNotFound {
			// Return empty list with error for test scenarios (as sometimes we want to test the error case)
			return []map[string]interface{}{}, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrResourceNotFound)
		}
		// Return error without empty list
		return nil, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrResourceNotFound)
	case http.StatusForbidden:
		if config.AllowForbidden {
			// Return empty list with error for test scenarios
			return []map[string]interface{}{}, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrAccessDenied)
		}
		// Return error without empty list
		return nil, fmt.Errorf("%s '%s' (status: %d): %w", config.ResourceIDType, config.ResourceID, resp.StatusCode, ErrAccessDenied)
	case http.StatusInternalServerError:
		// Server error - always return empty list and error for test scenarios
		return []map[string]interface{}{}, fmt.Errorf("reading %s for %s '%s' (status: %d): %s: %w", config.ResourceType, config.ResourceIDType, config.ResourceID, resp.StatusCode, string(respBody), ErrServerError)
	default:
		return nil, fmt.Errorf("status code %d: %w", resp.StatusCode, ErrUnexpectedStatus)
	}
}
