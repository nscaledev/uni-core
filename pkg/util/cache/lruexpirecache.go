/*
Copyright 2025 the Unikorn Authors.
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

package cache

import (
	"time"

	"github.com/brunoga/deep"

	"k8s.io/apimachinery/pkg/util/cache"
)

// LRUExpireCache is a drop in replacement for the apimachinery version
// that makes it typed with generics, and also data immutable via deep
// copies.
type LRUExpireCache[K comparable, T any] struct {
	cache *cache.LRUExpireCache
}

func NewLRUExpireCache[K comparable, T any](maxSize int) *LRUExpireCache[K, T] {
	return &LRUExpireCache[K, T]{
		cache: cache.NewLRUExpireCache(maxSize),
	}
}

func (c *LRUExpireCache[K, T]) Add(key K, value T, ttl time.Duration) {
	t, err := deep.Copy(value)
	if err != nil {
		return
	}

	c.cache.Add(key, t, ttl)
}

func (c *LRUExpireCache[K, T]) Get(key K) (T, bool) {
	var zero T

	value, ok := c.cache.Get(key)
	if !ok {
		return zero, false
	}

	typedValue, ok := value.(T)
	if !ok {
		return zero, false
	}

	t, err := deep.Copy(typedValue)
	if err != nil {
		return zero, false
	}

	return t, true
}
