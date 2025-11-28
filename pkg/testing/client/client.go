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
	return &APIClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client: &http.Client{
			Timeout: timeout,
		},
		authToken: authToken,
		config: Config{
			BaseURL:        baseURL,
			RequestTimeout: timeout,
			LogRequests:    false,
			LogResponses:   false,
		},
		logger: logger,
	}
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

// DoRequest performs an HTTP request with W3C trace context and returns the response.
// If expectedStatus is > 0, the function will return an error if the response status doesn't match.
//
//nolint:cyclop // complexity acceptable for generic HTTP client
func (c *APIClient) DoRequest(ctx context.Context, method, path string, body io.Reader, expectedStatus int) (*http.Response, []byte, error) {
	fullURL := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	// Add W3C Trace Context headers
	traceParent := CreateTraceParent()
	req.Header.Set("Traceparent", traceParent)
	req.Header.Set("Tracestate", "test-automation=true")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	duration := time.Since(start)

	if err != nil {
		c.logError(method, path, duration, traceParent, err, "http request failed")
		return nil, nil, fmt.Errorf("http request failed: %w", err)
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logErrorWithStatus(method, path, duration, resp.StatusCode, traceParent, err, "reading response body")
		return resp, nil, fmt.Errorf("reading response body: %w", err)
	}

	if c.config.LogRequests && c.logger != nil {
		c.logger.Printf("[%s %s] status=%d duration=%s traceparent=%s\n", method, path, resp.StatusCode, duration, traceParent)
	}

	if c.config.LogResponses && len(respBody) > 0 && c.logger != nil {
		c.logger.Printf("[%s %s] response body: %s\n", method, path, string(respBody))
	}

	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		c.logUnexpectedStatus(method, path, expectedStatus, resp.StatusCode, string(respBody), traceParent)
		return resp, respBody, fmt.Errorf("expected %d, got %d, body: %s (%s): %w", expectedStatus, resp.StatusCode, string(respBody), FormatTraceContext(traceParent), ErrUnexpectedStatusCode)
	}

	return resp, respBody, nil
}

// ListResource is a generic helper for list operations (for non-typed resources).
// NOTE: This uses map[string]interface{} for cross-service responses. For service-owned
// responses, prefer creating typed methods in your service client.
func (c *APIClient) ListResource(ctx context.Context, path string, config ResponseHandlerConfig) ([]map[string]interface{}, error) {
	//nolint:bodyclose // response body is closed in DoRequest
	resp, respBody, err := c.DoRequest(ctx, http.MethodGet, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", config.ResourceType, err)
	}

	return HandleResourceListResponse(resp, respBody, config)
}
