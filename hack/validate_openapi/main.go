/*
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

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	// extensionHidden is used to denote non-public APIs that don't have as
	// stringent requirements for documentation etc.
	// NOTE: x-internal is more commonly used, but we use Mintlfy so
	// x-hidden it is for now.
	extensionHidden = "x-hidden"
	// extensionNoSecurityRequirements is used to identify the endpoint has
	// no authentication/authorization.  It should only ever be used by APIs
	// that establish an authenticated workflow, or other utilities such as
	// API verisioning and service discovery.
	extensionNoSecurityRequirements = "x-no-security-requirements"
	// extensionNoAuthorization is used to identity an endpoint that cannot
	// return a 403 (forbidden) response, typically because it is used to
	// generate them in the first place e.g. an ACL.
	extensionNoAuthorization = "x-no-authorization"
	// extensionNoBody is used to indicate a POST or PUT that modify a resource
	// return no body, otherwise we enforce a body. media type and schema.
	extensionNoBody = "x-no-body"
)

// bold makes text pop in the console.
func bold(s string) string {
	return "\x1b[1m" + s + "\x1b[0m"
}

// red makes text look bad.
func red(s string) string {
	return "\x1b[1;31m" + s + "\x1b[0m"
}

// validationContext simply wraps up a bunch of common parameters to each
// validator and captures validation failures.
type validationContext struct {
	spec      *openapi3.T
	method    string
	path      *openapi3.PathItem
	operation *openapi3.Operation
	pathName  string

	failed   bool
	failures []string
}

// notify nof a failure and buffer it up.
func (c *validationContext) notify(v ...any) {
	c.failures = append(c.failures, fmt.Sprintln(v...))

	c.failed = true
}

// mustIncludeStatusCode ensures a status code is defined for the operation.
func (c *validationContext) mustIncludeStatusCode(code int) {
	if c.operation.Responses.Status(code) == nil {
		c.notify("status code", code, "should be defined")
	}
}

// report dumps any failures out to the console.
func (c *validationContext) report() {
	fmt.Println(bold(fmt.Sprint("Validation failure for endpoint ", c.method, " ", c.pathName)))

	for i := range c.failures {
		fmt.Print(red("*"), " ", c.failures[i])
	}

	fmt.Println()
}

// validationFunc is a common validation callback function allowing validators to
// be plug and play if so needed.
type validationFunc func(*validationContext)

// validateOperationDocumentation asserts something is explicitly undocumented, or it has
// a terse summary and a tag group.
func validateOperationDocumentation(c *validationContext) {
	if c.operation.Extensions[extensionHidden] != nil {
		return
	}

	if c.operation.Summary == "" {
		c.notify("no documentation summary provided")
	}

	if len(c.operation.Tags) == 0 {
		c.notify("no documentation tag grouping provided")
	}
}

// validateOperationSecurity asserts something is explciitly unauthenticated, or it has
// exactly one requirement, and 401 & 403 response defined.
func validateOperationSecurity(c *validationContext) {
	if _, ok := c.operation.Extensions[extensionNoSecurityRequirements]; ok {
		return
	}

	if c.operation.Security == nil {
		c.notify("no security requirements set")
		return
	}

	// If you have multiple, then the errors become ambiguous to handle.
	if len(*c.operation.Security) != 1 {
		c.notify("security requirements must have only one security requirement")
	}

	c.mustIncludeStatusCode(http.StatusUnauthorized)

	if _, ok := c.operation.Extensions[extensionNoAuthorization]; !ok {
		c.mustIncludeStatusCode(http.StatusForbidden)
	}
}

// validateOperationFoundness asserts that if operation parameters that are specified in the
// path, that aren't realted to RBAC (we never give any information away about identity), it
// has a 404 response defined.
func validateOperationFoundness(c *validationContext) {
	var parameters openapi3.Parameters

	parameters = append(parameters, c.path.Parameters...)
	parameters = append(parameters, c.operation.Parameters...)

	non404 := []string{
		"organizationID",
		"projectID",
	}

	parameters = slices.DeleteFunc(parameters, func(parameter *openapi3.ParameterRef) bool {
		return parameter.Value.In != openapi3.ParameterInPath || slices.Contains(non404, parameter.Value.Name)
	})

	if len(parameters) > 0 {
		c.mustIncludeStatusCode(http.StatusNotFound)
	}
}

// validateRequestValidation asserts all POST/PUT requests have a content type and schema defined.
func validateRequestValidation(c *validationContext) {
	if c.method != http.MethodPost && c.method != http.MethodPut {
		return
	}

	// You have to explicitly opt out from following the rules.
	if _, ok := c.operation.Extensions[extensionNoBody]; ok {
		return
	}

	// POST/PUT calls will have something to validate.
	if c.operation.RequestBody == nil {
		c.notify("no request body set")
	}

	body := c.operation.RequestBody.Value
	if body == nil {
		body = c.spec.Components.RequestBodies[c.operation.RequestBody.Ref].Value
	}

	// Request bodies will have a schema.
	for mimeType, mediaType := range body.Content {
		if mediaType.Schema == nil {
			c.notify("no request schema set for mime type", mimeType)
		}
	}
}

// validateResponseValidation asserts all GET responses have a content type and schema defined.
func validateResponseValidation(c *validationContext) {
	if c.method != http.MethodGet {
		return
	}

	for code := 100; code < 600; code++ {
		responseRef := c.operation.Responses.Status(code)
		if responseRef == nil {
			continue
		}

		if code != http.StatusOK {
			continue
		}

		response := responseRef.Value
		if response == nil {
			response = c.spec.Components.Responses[responseRef.Ref].Value
		}

		if response.Content == nil {
			c.notify("no content type set for", code, "response")
		}

		for mimeType, mediaType := range response.Content {
			if mimeType == "application/json" && mediaType.Schema == nil {
				c.notify("no schema set for", mimeType, code, "response")
			}
		}
	}
}

// validate simply runs through each validator in turn and prints out
// any violations encountered.
func validate(spec *openapi3.T) {
	validators := []validationFunc{
		validateOperationDocumentation,
		validateOperationSecurity,
		validateOperationFoundness,
		validateRequestValidation,
		validateResponseValidation,
	}

	var failed bool

	for _, pathName := range spec.Paths.InMatchingOrder() {
		path := spec.Paths.Find(pathName)

		for method, operation := range path.Operations() {
			for _, validator := range validators {
				validationContext := &validationContext{
					spec:      spec,
					method:    method,
					path:      path,
					pathName:  pathName,
					operation: operation,
				}

				validator(validationContext)

				if validationContext.failed {
					validationContext.report()

					failed = true
				}
			}
		}
	}

	if failed {
		os.Exit(1)
	}
}

func main() {
	var path string

	flag.StringVar(&path, "path", "pkg/openapi/server.spec.yaml", "Path to your OpenAPI schema")

	flag.Parse()

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	spec, err := loader.LoadFromFile(path)
	if err != nil {
		fmt.Println("failed to lead schema", err)
		os.Exit(1)
	}

	if err := spec.Validate(context.Background()); err != nil {
		fmt.Println("failed to validate spec", err)
		os.Exit(1)
	}

	validate(spec)
}
