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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrUnexpectedStatusCode indicates an unexpected HTTP status code was received.
	ErrUnexpectedStatusCode = errors.New("unexpected status code")
)

// Logger defines the interface for logging in the API client.
// This allows services to plug in their own logging implementation (e.g., ginkgo.GinkgoWriter).
type Logger interface {
	Printf(format string, args ...interface{})
}

// Config holds the base configuration for the API client.
type Config struct {
	BaseURL        string
	RequestTimeout time.Duration
	LogRequests    bool
	LogResponses   bool
}

// APIClient provides a generic HTTP client for API testing with trace context support.
type APIClient struct {
	baseURL   string
	client    *http.Client
	authToken string
	config    Config
	logger    Logger
}

// NewAPIClient creates a new API client with the given configuration.
func NewAPIClient(baseURL, authToken string, timeout time.Duration, logger Logger) *APIClient {
	config := Config{
		BaseURL:        baseURL,
		RequestTimeout: timeout,
		LogRequests:    false,
		LogResponses:   false,
	}

	return NewAPIClientWithConfig(config, authToken, logger)
}

// NewAPIClientWithConfig creates a new API client with the given configuration struct.
func NewAPIClientWithConfig(config Config, authToken string, logger Logger) *APIClient {
	return &APIClient{
		baseURL: strings.TrimSuffix(config.BaseURL, "/"),
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		authToken: authToken,
		config:    config,
		logger:    logger,
	}
}

// SetAuthToken sets the authentication token for the client.
func (c *APIClient) SetAuthToken(token string) {
	c.authToken = token
}

// SetLogRequests enables or disables request logging.
func (c *APIClient) SetLogRequests(enabled bool) {
	c.config.LogRequests = enabled
}

// SetLogResponses enables or disables response logging.
func (c *APIClient) SetLogResponses(enabled bool) {
	c.config.LogResponses = enabled
}

// logError logs a generic error with trace context.
func (c *APIClient) logError(method, path string, duration time.Duration, traceParent string, err error, context string) {
	if c.logger != nil {
		c.logger.Printf("[%s %s] ERROR %s duration=%s %s error=%v\n", method, path, context, duration, FormatTraceContext(traceParent), err)
		c.logTraceContext(traceParent)
	}
}

// logErrorWithStatus logs an error with HTTP status code.
func (c *APIClient) logErrorWithStatus(method, path string, duration time.Duration, statusCode int, traceParent string, err error, context string) {
	if c.logger != nil {
		c.logger.Printf("[%s %s] ERROR %s duration=%s status=%d %s error=%v\n", method, path, context, duration, statusCode, FormatTraceContext(traceParent), err)
		c.logTraceContext(traceParent)
	}
}

// logUnexpectedStatus logs an unexpected HTTP status code.
func (c *APIClient) logUnexpectedStatus(method, path string, expectedStatus, actualStatus int, body, traceParent string) {
	if c.logger != nil {
		c.logger.Printf("[%s %s] UNEXPECTED STATUS expected=%d got=%d body=%s %s\n", method, path, expectedStatus, actualStatus, body, FormatTraceContext(traceParent))
		c.logTraceContext(traceParent)
	}
}

// logTraceContext logs the trace context information.
func (c *APIClient) logTraceContext(traceParent string) {
	if c.logger != nil {
		c.logger.Printf("TRACE CONTEXT: Use %s to search logs for this request\n", FormatTraceContext(traceParent))
	}
}

// buildHTTPRequest creates an HTTP request with trace context and authentication headers.
func (c *APIClient) buildHTTPRequest(ctx context.Context, method, fullURL string, body io.Reader) (*http.Request, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	traceParent := CreateTraceParent()
	req.Header.Set("Traceparent", traceParent)
	req.Header.Set("Tracestate", "test-automation=true")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	return req, traceParent, nil
}

// executeHTTPRequest executes an HTTP request with timing and error handling.
func (c *APIClient) executeHTTPRequest(req *http.Request, method, path, traceParent string) (*http.Response, time.Duration, error) {
	start := time.Now()
	resp, err := c.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		c.logError(method, path, duration, traceParent, err, "http request failed")
		return nil, duration, fmt.Errorf("http request failed: %w", err)
	}

	return resp, duration, nil
}

// readResponseBody reads the response body with error handling.
func (c *APIClient) readResponseBody(resp *http.Response, method, path, traceParent string, duration time.Duration) ([]byte, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logErrorWithStatus(method, path, duration, resp.StatusCode, traceParent, err, "reading response body")
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return respBody, nil
}

// logRequestResponse logs the request and response if logging is enabled.
func (c *APIClient) logRequestResponse(method, path string, statusCode int, duration time.Duration, traceParent string, respBody []byte) {
	if c.config.LogRequests && c.logger != nil {
		c.logger.Printf("[%s %s] status=%d duration=%s traceparent=%s\n", method, path, statusCode, duration, traceParent)
	}

	if c.config.LogResponses && len(respBody) > 0 && c.logger != nil {
		c.logger.Printf("[%s %s] response body: %s\n", method, path, string(respBody))
	}
}

// validateStatusCode validates the response status code against the expected value.
func (c *APIClient) validateStatusCode(method, path string, expectedStatus, actualStatus int, respBody []byte, traceParent string) error {
	if expectedStatus > 0 && actualStatus != expectedStatus {
		c.logUnexpectedStatus(method, path, expectedStatus, actualStatus, string(respBody), traceParent)
		return fmt.Errorf("expected %d, got %d, body: %s (%s): %w", expectedStatus, actualStatus, string(respBody), FormatTraceContext(traceParent), ErrUnexpectedStatusCode)
	}

	return nil
}

// DoRequest performs an HTTP request with W3C trace context and returns the response.
// If expectedStatus is > 0, the function will return an error if the response status doesn't match.
func (c *APIClient) DoRequest(ctx context.Context, method, path string, body io.Reader, expectedStatus int) (*http.Response, []byte, error) {
	fullURL := c.baseURL + path

	req, traceParent, err := c.buildHTTPRequest(ctx, method, fullURL, body)
	if err != nil {
		return nil, nil, err
	}

	resp, duration, err := c.executeHTTPRequest(req, method, path, traceParent)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := c.readResponseBody(resp, method, path, traceParent, duration)
	if err != nil {
		return resp, nil, err
	}

	c.logRequestResponse(method, path, resp.StatusCode, duration, traceParent, respBody)

	if err := c.validateStatusCode(method, path, expectedStatus, resp.StatusCode, respBody, traceParent); err != nil {
		return resp, respBody, err
	}

	return resp, respBody, nil
}

// ListResource is a type-safe generic helper for list operations.
// Type parameter T should be the element type (e.g., openapi.Cluster).
// Example usage: ListResource[openapi.Cluster](ctx, client, path, config).
func ListResource[T any](ctx context.Context, c *APIClient, path string, config ResponseHandlerConfig) ([]T, error) {
	var zero []T
	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return zero, fmt.Errorf("listing %s: %w", config.ResourceType, err)
	}

	return HandleResourceListResponse[T](resp, respBody, config)
}
