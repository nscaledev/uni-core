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

package util

import (
	"unicode"

	"k8s.io/apimachinery/pkg/util/uuid"
)

// GenerateResourceID creates a valid Kubernetes name from a UUID.
func GenerateResourceID() string {
	for {
		// NOTE: Kubernetes UUIDs are based on version 4, aka random,
		// so the first character will be a letter eventually, like
		// a 6/16 chance: tl;dr infinite loops are... improbable.
		if id := uuid.NewUUID(); unicode.IsLetter(rune(id[0])) {
			return string(id)
		}
	}
}
