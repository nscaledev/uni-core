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

package reservedtags_test

import (
	"slices"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	servererrors "github.com/unikorn-cloud/core/pkg/server/errors"
	"github.com/unikorn-cloud/core/pkg/server/reservedtags"
)

const (
	k8sPrefix     = "k8s.example.org/"
	k8sPermission = "k8s:tags"
)

func options() *reservedtags.Options {
	return &reservedtags.Options{
		Prefixes: map[string]string{
			k8sPrefix:            k8sPermission,
			"infra.example.org/": "infra:tags",
		},
	}
}

func holding(permissions ...string) reservedtags.Holds {
	return func(permission string) bool {
		return slices.Contains(permissions, permission)
	}
}

func tags(pairs ...string) unikornv1core.TagList {
	out := unikornv1core.TagList{}

	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, unikornv1core.Tag{Name: pairs[i], Value: pairs[i+1]})
	}

	return out
}

func TestFlag(t *testing.T) {
	t.Parallel()

	o := &reservedtags.Options{}
	f := pflag.NewFlagSet("", pflag.ContinueOnError)
	o.AddFlags(f)

	require.NoError(t, f.Parse([]string{"--reserved-tag-prefixes=k8s.example.org/=k8s:tags,infra.example.org/=k8s:tags"}))
	assert.Equal(t, map[string]string{"k8s.example.org/": "k8s:tags", "infra.example.org/": "k8s:tags"}, o.Prefixes)
	require.NoError(t, o.Validate())
}

func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		prefixes map[string]string
		err      error
	}{
		{name: "none", prefixes: nil},
		{name: "disjoint", prefixes: map[string]string{"a/": "x", "b/": "x"}},
		{name: "empty prefix", prefixes: map[string]string{"": "x"}, err: reservedtags.ErrEmptyPrefix},
		{name: "empty permission", prefixes: map[string]string{"a/": ""}, err: reservedtags.ErrEmptyPermission},
		{name: "nested", prefixes: map[string]string{"a/": "x", "a/b/": "y"}, err: reservedtags.ErrOverlap},
		{name: "case variant", prefixes: map[string]string{"a/": "x", "A/": "x"}, err: reservedtags.ErrOverlap},
		{name: "leading space", prefixes: map[string]string{" a/": "x"}, err: reservedtags.ErrMalformed},
		{name: "trailing space permission", prefixes: map[string]string{"a/": "x "}, err: reservedtags.ErrMalformed},
		{name: "prefix split at =", prefixes: map[string]string{"a": "b/=x"}, err: reservedtags.ErrMalformed},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := (&reservedtags.Options{Prefixes: c.prefixes}).Validate()
			if c.err == nil {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, c.err)
		})
	}
}

func TestOnCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		tags   unikornv1core.TagList
		holds  reservedtags.Holds
		denied string
	}{
		{name: "no tags", tags: nil, holds: holding()},
		{name: "unreserved", tags: tags("env", "prod"), holds: holding()},
		{name: "owner", tags: tags("k8s.example.org/cluster", "c1"), holds: holding(k8sPermission)},
		{name: "non-owner", tags: tags("env", "prod", "k8s.example.org/cluster", "c1"), holds: holding(), denied: "k8s.example.org/cluster"},
		{name: "case folded", tags: tags("K8S.Example.org/cluster", "c1"), holds: holding(), denied: "K8S.Example.org/cluster"},
		{name: "owns one prefix only", tags: tags("k8s.example.org/a", "1", "infra.example.org/b", "2"), holds: holding(k8sPermission), denied: "infra.example.org/b"},
		{name: "bare prefix is reserved", tags: tags("k8s.example.org/", "1"), holds: holding(), denied: "k8s.example.org/"},
		{name: "shorter than prefix", tags: tags("k8s", "1"), holds: holding()},
		{name: "kelvin sign folds to k", tags: tags("\u212a8s.example.org/x", "1"), holds: holding(), denied: "8s.example.org/x"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := options().OnCreate(t.Context(), c.tags, c.holds)
			if c.denied == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.True(t, servererrors.IsForbidden(err))
			assert.Contains(t, err.Error(), c.denied)
		})
	}
}

func TestOnUpdate(t *testing.T) {
	t.Parallel()

	stored := tags("env", "prod", "k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1")

	cases := []struct {
		name     string
		incoming unikornv1core.TagList
		holds    reservedtags.Holds
		want     unikornv1core.TagList
	}{
		{
			name:     "non-owner omits reserved tags, they are kept",
			incoming: tags("env", "dev"),
			holds:    holding(),
			want:     tags("env", "dev", "k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1"),
		},
		{
			name:     "non-owner changes a value, stored value kept",
			incoming: tags("env", "prod", "k8s.example.org/cluster", "evil", "infra.example.org/pool", "p1"),
			holds:    holding(),
			want:     tags("env", "prod", "k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1"),
		},
		{
			name:     "non-owner adds a reserved tag, dropped",
			incoming: tags("k8s.example.org/new", "x"),
			holds:    holding(),
			want:     tags("k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1"),
		},
		{
			name:     "non-owner faithful round trip",
			incoming: stored,
			holds:    holding(),
			want:     tags("env", "prod", "k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1"),
		},
		{
			name:     "non-owner case variant is reserved",
			incoming: tags("K8S.example.org/cluster", "evil"),
			holds:    holding(),
			want:     tags("k8s.example.org/cluster", "c1", "infra.example.org/pool", "p1"),
		},
		{
			name:     "owner removes by omission",
			incoming: tags("env", "prod"),
			holds:    holding(k8sPermission, "infra:tags"),
			want:     tags("env", "prod"),
		},
		{
			name:     "owner changes a value",
			incoming: tags("k8s.example.org/cluster", "c2", "infra.example.org/pool", "p1"),
			holds:    holding(k8sPermission, "infra:tags"),
			want:     tags("k8s.example.org/cluster", "c2", "infra.example.org/pool", "p1"),
		},
		{
			name:     "owns one prefix, judged separately",
			incoming: tags("k8s.example.org/cluster", "c2", "infra.example.org/pool", "evil"),
			holds:    holding(k8sPermission),
			want:     tags("k8s.example.org/cluster", "c2", "infra.example.org/pool", "p1"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, options().OnUpdate(t.Context(), stored, c.incoming, c.holds))
		})
	}
}

func TestOnUpdateNothingReserved(t *testing.T) {
	t.Parallel()

	incoming := tags("k8s.example.org/cluster", "c2")
	never := func(string) bool { t.Fatal("holds called with nothing reserved"); return false }

	got := (&reservedtags.Options{}).OnUpdate(t.Context(), tags("k8s.example.org/cluster", "c1"), incoming, never)
	assert.Equal(t, incoming, got)
}

func TestOnUpdateDuplicateNames(t *testing.T) {
	t.Parallel()

	stored := tags("k8s.example.org/a", "1", "k8s.example.org/a", "2")

	got := options().OnUpdate(t.Context(), stored, tags("k8s.example.org/a", "1"), holding())
	assert.Equal(t, stored, got)
}
