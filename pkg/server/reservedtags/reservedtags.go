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

// Package reservedtags lets an operator reserve tag prefixes for the holders
// of an RBAC endpoint. See README.md.
package reservedtags

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/pflag"

	unikornv1core "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	servererrors "github.com/unikorn-cloud/core/pkg/server/errors"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrEmptyPrefix   = errors.New("reserved tag prefix is empty")
	ErrEmptyEndpoint = errors.New("reserved tag prefix has no endpoint")
	ErrOverlap       = errors.New("reserved tag prefixes overlap")
	ErrMalformed     = errors.New("reserved tag prefix is malformed")
)

// Options maps each reserved tag prefix to the RBAC endpoint whose holders own it.
type Options struct {
	Prefixes map[string]string
}

func (o *Options) AddFlags(f *pflag.FlagSet) {
	f.StringToStringVar(&o.Prefixes, "reserved-tag-prefixes", nil, "Reserved tag prefixes, as prefix=endpoint pairs. Only callers holding the endpoint may write tags under the prefix.")
}

// Validate refuses empty or malformed entries, and prefixes that overlap, so
// no tag can match more than one prefix.
func (o *Options) Validate() error {
	for prefix, endpoint := range o.Prefixes {
		if prefix == "" {
			return ErrEmptyPrefix
		}

		if endpoint == "" {
			return fmt.Errorf("%w: %q", ErrEmptyEndpoint, prefix)
		}

		// Catches a space after a comma, or a prefix containing "=" that
		// pflag split early, either of which would reserve the wrong thing.
		if strings.TrimSpace(prefix) != prefix || strings.TrimSpace(endpoint) != endpoint || strings.Contains(endpoint, "=") {
			return fmt.Errorf("%w: %q=%q", ErrMalformed, prefix, endpoint)
		}

		for other := range o.Prefixes {
			if other != prefix && hasPrefixFold(other, prefix) {
				return fmt.Errorf("%w: %q and %q", ErrOverlap, prefix, other)
			}
		}
	}

	return nil
}

// Holds reports whether the caller holds the endpoint for the operation being
// served, at the scope of the resource being written. It must not record a
// refusal: a false answer is a normal outcome, not a denied request.
type Holds func(endpoint string) bool

// OnCreate refuses, with 403, a create carrying a tag under a prefix the
// caller does not own.
func (o *Options) OnCreate(ctx context.Context, tags unikornv1core.TagList, holds Holds) error {
	for _, tag := range tags {
		prefix, endpoint, ok := o.match(tag.Name)
		if !ok || holds(endpoint) {
			continue
		}

		log.FromContext(ctx).Info("reserved tag rejected on create", "prefix", prefix, "tag", tag.Name)

		return servererrors.HTTPForbidden(fmt.Sprintf("tag %q is reserved", tag.Name))
	}

	return nil
}

// OnUpdate returns the tags to store. Under a prefix the caller owns, the
// request decides. Under one it does not own, the stored tags are kept and
// the request is ignored. The result is the accepted request tags in request
// order, then the kept stored tags in stored order.
func (o *Options) OnUpdate(ctx context.Context, stored, incoming unikornv1core.TagList, holds Holds) unikornv1core.TagList {
	if len(o.Prefixes) == 0 {
		return incoming
	}

	out := make(unikornv1core.TagList, 0, len(incoming))

	// Prefixes where a non-owner tried to add a tag or change a value.
	changed := map[string]bool{}

	for _, tag := range incoming {
		prefix, endpoint, ok := o.match(tag.Name)
		if !ok || holds(endpoint) {
			out = append(out, tag)
			continue
		}

		if !stored.Contains(tag) {
			changed[prefix] = true
		}
	}

	for _, tag := range stored {
		if _, endpoint, ok := o.match(tag.Name); ok && !holds(endpoint) {
			out = append(out, tag)
		}
	}

	for prefix := range changed {
		log.FromContext(ctx).Info("reserved tag change ignored on update", "prefix", prefix)
	}

	return out
}

// match returns the reserved prefix the tag name falls under, and its endpoint.
// Validate guarantees at most one prefix matches.
func (o *Options) match(name string) (string, string, bool) {
	for prefix, endpoint := range o.Prefixes {
		if hasPrefixFold(name, prefix) {
			return prefix, endpoint, true
		}
	}

	return "", "", false
}

// hasPrefixFold is strings.HasPrefix under Unicode simple case folding, as
// strings.EqualFold does. Rune by rune, because folded runes can differ in
// byte length.
func hasPrefixFold(s, prefix string) bool {
	for prefix != "" {
		if s == "" {
			return false
		}

		pr, pn := utf8.DecodeRuneInString(prefix)
		sr, sn := utf8.DecodeRuneInString(s)

		if !strings.EqualFold(string(pr), string(sr)) {
			return false
		}

		prefix, s = prefix[pn:], s[sn:]
	}

	return true
}
