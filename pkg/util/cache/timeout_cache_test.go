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

package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/util/cache"
)

func testPresent(t *testing.T, cache *cache.TimeoutCache[int], expected int) {
	t.Helper()

	actual, ok := cache.Get()
	require.True(t, ok)
	require.Equal(t, expected, actual)
}

func testAbsent(t *testing.T, cache *cache.TimeoutCache[int]) {
	t.Helper()

	actual, ok := cache.Get()
	require.False(t, ok)
	require.Zero(t, actual)
}

func TestTimeoutCache_Invalidate(t *testing.T) {
	t.Parallel()

	timeoutCache := cache.New[int](time.Hour)

	testAbsent(t, timeoutCache)

	expected := 1024
	timeoutCache.Set(expected)
	testPresent(t, timeoutCache, expected)

	timeoutCache.Invalidate()
	testAbsent(t, timeoutCache)
}

type staticClock struct {
	time time.Time
}

func newStaticClock() *staticClock {
	return &staticClock{time.Now()}
}

func (c *staticClock) Now() time.Time {
	return c.time
}

func (c *staticClock) advance(d time.Duration) {
	c.time = c.time.Add(d)
}

func TestTimeoutCache_Timeout(t *testing.T) {
	t.Parallel()

	c := newStaticClock()

	timeoutCache := cache.NewWithClock[int](time.Hour, c)
	testAbsent(t, timeoutCache)

	expected := 65535
	timeoutCache.Set(expected)
	testPresent(t, timeoutCache, expected)

	c.advance(61 * time.Minute)
	testAbsent(t, timeoutCache)
}

func TestTimeoutCache_SetResetsTimeout(t *testing.T) {
	t.Parallel()

	c := newStaticClock()

	timeoutCache := cache.NewWithClock[int](time.Hour, c)
	testAbsent(t, timeoutCache)

	expected := 8
	timeoutCache.Set(expected)
	testPresent(t, timeoutCache, expected)

	c.advance(61 * time.Minute)
	testAbsent(t, timeoutCache)

	timeoutCache.Set(expected)
	testPresent(t, timeoutCache, expected)
}
