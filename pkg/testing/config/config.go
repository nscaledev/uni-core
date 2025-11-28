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

package config

import (
	"time"
)

// BaseConfig contains the base configuration fields common across all API test clients.
// Services should embed this struct and add service-specific fields.
type BaseConfig struct {
	BaseURL         string
	AuthToken       string
	RequestTimeout  time.Duration
	TestTimeout     time.Duration
	SkipIntegration bool
	DebugLogging    bool
	LogRequests     bool
	LogResponses    bool
}

// NewBaseConfig creates a new BaseConfig with default values.
func NewBaseConfig() *BaseConfig {
	return &BaseConfig{
		RequestTimeout:  30 * time.Second,
		TestTimeout:     20 * time.Minute,
		SkipIntegration: false,
		DebugLogging:    false,
		LogRequests:     false,
		LogResponses:    false,
	}
}
