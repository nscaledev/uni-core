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

package util_test

import (
	"testing"
	"unicode"

	"github.com/google/uuid"

	"github.com/unikorn-cloud/core/pkg/util"
)

func TestGenerateDeterministicResourceID_Deterministic(t *testing.T) {
	t.Parallel()

	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	a := util.GenerateDeterministicResourceID(ns, "foo:bar")
	b := util.GenerateDeterministicResourceID(ns, "foo:bar")

	if a != b {
		t.Errorf("expected identical outputs for same inputs, got %q and %q", a, b)
	}
}

func TestGenerateDeterministicResourceID_StartsWithLetter(t *testing.T) {
	t.Parallel()

	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	for _, invariant := range []string{"", "foo", "a:b:c", "123", "network-id:server-name"} {
		id := util.GenerateDeterministicResourceID(ns, invariant)
		if !unicode.IsLetter(rune(id[0])) {
			t.Errorf("id %q for invariant %q does not start with a letter", id, invariant)
		}
	}
}

func TestGenerateDeterministicResourceID_DifferentInvariantsYieldDifferentIDs(t *testing.T) {
	t.Parallel()

	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	a := util.GenerateDeterministicResourceID(ns, "foo:bar")
	b := util.GenerateDeterministicResourceID(ns, "foo:baz")

	if a == b {
		t.Errorf("expected different outputs for different invariants, both got %q", a)
	}
}

func TestGenerateDeterministicResourceID_DifferentNamespacesYieldDifferentIDs(t *testing.T) {
	t.Parallel()

	nsA := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	nsB := uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8")

	a := util.GenerateDeterministicResourceID(nsA, "foo:bar")
	b := util.GenerateDeterministicResourceID(nsB, "foo:bar")

	if a == b {
		t.Errorf("expected different outputs for different namespaces, both got %q", a)
	}
}

// TestGenerateDeterministicResourceID_Golden pins the exact output values so that
// any accidental change to the hashing formula (byte encoding, fallback strategy,
// etc.) causes a test failure before breaking resources already stored in Kubernetes.
// "foo:bar" resolves on the first attempt (standard uuid5); "foo:baz" requires one
// fallback iteration (bare uuid5 starts with a digit, so the previous UUID bytes are
// rehashed).
func TestGenerateDeterministicResourceID_Golden(t *testing.T) {
	t.Parallel()

	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	cases := []struct {
		invariant string
		want      string
	}{
		// Resolves on first attempt: pure uuid5(ns, invariant).
		{"foo:bar", "fd088d96-7c6b-5adf-8581-cbb18a5dad67"},
		// Requires fallback: bare uuid5 starts with a digit; rehashes previous UUID bytes.
		{"foo:baz", "fae08b48-6d31-57e0-ba2d-1619034aaae7"},
	}

	for _, c := range cases {
		got := util.GenerateDeterministicResourceID(ns, c.invariant)
		if got != c.want {
			t.Errorf("invariant %q: got %q, want %q", c.invariant, got, c.want)
		}
	}
}
