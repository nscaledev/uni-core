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

package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var errConfigFileNotFound = viper.ConfigFileNotFoundError{}

// Error represents a configuration error with missing fields.
type Error struct {
	missing string
}

func (e *Error) Error() string {
	return fmt.Sprintf("missing required configuration: %s. Please set these environment variables or add them to a .env file, or the gh secrets", e.missing)
}

// GetDurationFromViper safely extracts a duration from viper, handling both duration strings and integer seconds.
func GetDurationFromViper(v *viper.Viper, key string, defaultValue time.Duration) time.Duration {
	duration := v.GetDuration(key)
	if duration < time.Millisecond {
		seconds := v.GetInt(key)
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	if duration > 0 {
		return duration
	}

	return defaultValue
}

// ValidateRequiredFields checks that all required configuration values are set.
// Takes a map of environment variable names to their values.
// Returns an Error if any required fields are missing.
func ValidateRequiredFields(required map[string]string) error {
	var missing []string

	for envVar, value := range required {
		if value == "" {
			missing = append(missing, envVar)
		}
	}

	if len(missing) > 0 {
		return &Error{missing: strings.Join(missing, ", ")}
	}

	return nil
}

// SetupViper creates and configures a new Viper instance for loading test configuration.
// configName: name of the config file (e.g., ".env")
// configPaths: paths to search for the config file
// defaults: default values to set
func SetupViper(configName string, configPaths []string, defaults map[string]interface{}) (*viper.Viper, error) {
	v := viper.New()

	// Set up config file search paths
	v.SetConfigName(configName)
	v.SetConfigType("env")

	for _, path := range configPaths {
		v.AddConfigPath(path)
	}

	// Set default values
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	if err := v.ReadInConfig(); err != nil {
		// Only warn if it's not a "file not found" error
		if !errors.As(err, &errConfigFileNotFound) {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	v.AutomaticEnv()

	return v, nil
}
