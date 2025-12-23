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

package contract

import (
	"fmt"

	"github.com/pact-foundation/pact-go/v2/consumer"
	"github.com/pact-foundation/pact-go/v2/log"
)

// PactConfig holds configuration for Pact contract testing.
type PactConfig struct {
	// Consumer is the name of the service consuming the API
	Consumer string
	// Provider is the name of the service providing the API
	Provider string
	// PactDir is the directory where pact files will be written
	PactDir string
}

// NewPact creates a new Pact mock provider for consumer contract testing.
// This is a thin wrapper around pact-go/v2 that integrates with Ginkgo.
func NewPact(config PactConfig) (*consumer.V2HTTPMockProvider, error) {
	// Set pact logging to error level to reduce noise in test output
	if err := log.SetLogLevel("ERROR"); err != nil {
		return nil, fmt.Errorf("setting pact log level: %w", err)
	}

	pact, err := consumer.NewV2Pact(consumer.MockHTTPProviderConfig{
		Consumer: config.Consumer,
		Provider: config.Provider,
		PactDir:  config.PactDir,
	})

	if err != nil {
		return nil, fmt.Errorf("creating pact provider: %w", err)
	}

	return pact, nil
}
