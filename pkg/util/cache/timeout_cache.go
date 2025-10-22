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

package cache

import (
	"sync"
	"time"
)

// TimeoutCache provides a cache with timeout.
type TimeoutCache[V any] struct {
	value   V
	refresh time.Duration
	invalid time.Time
	rwlock  sync.RWMutex
}

// New gets a new cache.
func New[V any](refresh time.Duration) *TimeoutCache[V] {
	return &TimeoutCache[V]{
		refresh: refresh,
	}
}

// Get returns the cached value if set and it hasn't timed out
// and returns true.  If it has timed out, it will return V's
// zero value and false, and will need to be set again.
//
//nolint:nonamedreturns
func (m *TimeoutCache[V]) Get() (value V, ok bool) {
	m.rwlock.RLock()
	defer m.rwlock.RUnlock()

	if time.Now().After(m.invalid) {
		return
	}

	return m.value, true
}

// Set remembers the value and resets the invalid time based
// on when the cache was set.
func (m *TimeoutCache[V]) Set(value V) {
	m.rwlock.Lock()
	defer m.rwlock.Unlock()
	m.invalid = time.Now().Add(m.refresh)
	m.value = value
}

func (m *TimeoutCache[V]) Invalidate() {
	m.invalid = time.Time{}
}
