/*
Copyright 2022-2024 EscherCloud.
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

package errors

import (
	"errors"
)

var (
	// ErrParseFlag is raised when flag parsing fails.
	ErrParseFlag = errors.New("flag was unable to be parsed")

	// ErrCDDriver is raised when a CD driver is not handled.
	ErrCDDriver = errors.New("unhandled CD driver")

	// ErrInvalidContext is raised when the context is not correctly polulated.
	ErrInvalidContext = errors.New("context invalid")

	// ErrKubeconfig is raised wne the Kubeconfig isn't correct.
	ErrKubeconfig = errors.New("kubeconfig error")

	// ErrSecretFormatError is returned when a secret doesn't meet the specification.
	ErrSecretFormatError = errors.New("secret incorrectly formatted")

	// ErrAPIStatus is returned when an API status code is unexpected.
	ErrAPIStatus = errors.New("api status code unexpected")

	// ErrConsistency is returned when there is a consistency error, akin to an assert.
	ErrConsistency = errors.New("consistency error")

	// ErrUnsupportedKeyType s returned when a cryptographic key type is unsupported.
	ErrUnsupportedKeyType = errors.New("private key type is unsupported")

	// ErrKey is returned when a required map key is not defined.  This is typically
	// used when a data key in a secret is required, but missing.
	ErrKey = errors.New("key error")

	// ErrTypeConversion is thrown when a type assertion goes wrong.
	ErrTypeConversion = errors.New("type conversion error")

	// ErrResourceNotFound is thrown when a resource cannot be found.
	ErrResourceNotFound = errors.New("resource not found")

	// ErrConflict is thrown when a resource conflicts with another.
	ErrConflict = errors.New("resource conflict")
)
