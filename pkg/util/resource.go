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

	"github.com/google/uuid"

	k8suuid "k8s.io/apimachinery/pkg/util/uuid"
)

// GenerateResourceID creates a valid Kubernetes name from a UUID.
func GenerateResourceID() string {
	for {
		// NOTE: Kubernetes UUIDs are based on version 4, aka random,
		// so the first character will be a letter eventually, like
		// a 6/16 chance: tl;dr infinite loops are... improbable.
		if id := k8suuid.NewUUID(); unicode.IsLetter(rune(id[0])) {
			return string(id)
		}
	}
}

// GenerateDeterministicResourceID derives a valid Kubernetes name from a UUID v5
// (SHA-1) hash of idNamespace and invariant. On the first attempt the standard
// uuid5(idNamespace, invariant) is returned if it starts with a letter; otherwise
// uuid5(idNamespace, previousUUID[:]) is iteratively applied until the constraint
// is met. Fallbacks hash binary UUID bytes rather than strings, keeping the fallback
// inputs out of the invariant string space and ensuring no two distinct invariants
// can ever produce the same name.
func GenerateDeterministicResourceID(idNamespace uuid.UUID, invariant string) string {
	id := uuid.NewSHA1(idNamespace, []byte(invariant))

	for !unicode.IsLetter(rune(id.String()[0])) {
		// Rehash the previous UUID's raw bytes rather than constructing a
		// string variant of the invariant (e.g. "foo:1", "foo:2"). String
		// variants share the invariant's namespace and can collide: if the
		// bare "foo:baz" resolved via "foo:baz:3", then an invariant literally
		// named "foo:baz:3" would produce the same Kubernetes name. Binary
		// UUID bytes can never be produced by a UTF-8 invariant string, so
		// the two input spaces are disjoint and collisions are structurally
		// impossible.
		// UUID strings are lowercase hex; unicode.IsLetter accepts a-f and
		// rejects 0-9. SHA-1's first nibble is uniformly distributed, so
		// each attempt has a 6/16 (~37.5%) chance of success and the loop
		// typically exits within 2-3 iterations.
		id = uuid.NewSHA1(idNamespace, id[:])
	}

	return id.String()
}
