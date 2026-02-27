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

// # Benchmarking
//
// Any benchmarks need to be run for an extended period of time to
// ensure refreshes are invisible from the client.  Where time per
// operation is specified this was done on a:
//
//	12th Gen Intel(R) Core(TM) i7-1270P
//
// With the following command:
//
//	go test -v -bench . -benchtime 10s ./pkg/util/cache/...
package cache_test

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/util/cache"
)

// myType is a fake struct.  Irrespective of the size of this it should
// only every be referred to by reference, so should make zero difference
// in performance.
type myType struct {
	id int
}

func (t *myType) Index() string {
	return strconv.Itoa(t.id)
}

// Equal implements the Equatable interface.
func (t *myType) Equal(o *myType) bool {
	return *t == *o
}

// staticGenerator provides a way to generate non changing data for the cache.
type staticGenerator struct {
	// size of the dataset.
	size int
	// warmup records whether we have seen the warm up invocation yet.
	warmup bool
}

// refresh generates a dataset.
func (g *staticGenerator) refresh(_ context.Context) ([]*myType, error) {
	items := make([]*myType, g.size)

	for i := range items {
		items[i] = &myType{
			id: i,
		}
	}

	if !g.warmup {
		time.Sleep(time.Second)

		g.warmup = true
	}

	return items, nil
}

// incrementingGenerator provides a way to generate changing data for the cache.
type incrementingGenerator struct {
	// size of the dataset.
	size int
	// generation is an incrementer than affects the dataset with
	// each invocation, and should trigger epoch recalculation.
	generation int
}

// refresh generates a dataset.
func (g *incrementingGenerator) refresh(_ context.Context) ([]*myType, error) {
	items := make([]*myType, g.size)

	for i := range items {
		items[i] = &myType{
			id: g.generation + i,
		}
	}

	// Add a deliberate delay in here to ensure readers aren't
	// impacted.  The conditional logic makes it only happen after
	// cache warmup.
	if g.generation != 0 {
		time.Sleep(time.Second)
	}

	g.generation++

	return items, nil
}

// defaultOptions returns a standard set of options for testing.
// This is closely tied to the generator implementation so we
// are ostensibly constantly performing refreshes.
func defaultOptions() *cache.RefreshAheadCacheOptions {
	return &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Second,
	}
}

// TestEpochUpdate checks that the cache data visibly changes its epoch
// when the data changes.
func TestEpochUpdate(t *testing.T) {
	t.Parallel()

	generator := incrementingGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	snapshot1, err := c.List()
	require.NoError(t, err)

	// It'll take one tick to trigger the refresh, and that itself will take another
	// tick, so 3 will be enough to witness the change.
	time.Sleep(3 * time.Second)

	snapshot2, err := c.List()
	require.NoError(t, err)

	require.False(t, snapshot1.Epoch.Valid(snapshot2.Epoch))
}

// TestEpochStability checks that the cache data doesn't visibly change
// its epoch when the data doesn't change.
func TestEpochStability(t *testing.T) {
	t.Parallel()

	generator := staticGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	snapshot1, err := c.List()
	require.NoError(t, err)

	// It'll take one tick to trigger the refresh, and that itself will take another
	// tick, so 3 will be enough to witness the change.
	time.Sleep(3 * time.Second)

	snapshot2, err := c.List()
	require.NoError(t, err)

	require.True(t, snapshot1.Epoch.Valid(snapshot2.Epoch))
}

// TestListImmutability ensures modifying a list result in a destructive way
// does not affect the cache.
func TestListImmutability(t *testing.T) {
	t.Parallel()

	generator := staticGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	snapshot1, err := c.List()
	require.NoError(t, err)

	// DeleteFunc will zero out the underlying array, destroying any discarded data.
	items := slices.DeleteFunc(snapshot1.Items, func(item *myType) bool {
		return item.id%2 == 0
	})

	require.Len(t, items, 512)

	snapshot2, err := c.List()
	require.NoError(t, err)
	require.Len(t, snapshot2.Items, 1024)

	for i := range snapshot2.Items {
		require.NotNil(t, snapshot2.Items[i])
	}
}

// TestInvalidation tests that a client can invalidate the cache and that
// the client is blocked until completion.
func TestInvalidation(t *testing.T) {
	t.Parallel()

	generator := incrementingGenerator{size: 1024}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	snapshot1, err := c.List()
	require.NoError(t, err)

	require.NoError(t, c.Invalidate())

	snapshot2, err := c.List()
	require.NoError(t, err)

	require.False(t, snapshot1.Epoch.Valid(snapshot2.Epoch))
}

// TestInvavalidationErrors checks that invalidation gracefully handles
// shutdown.
func TestInvavalidationErrors(t *testing.T) {
	t.Parallel()

	generator := incrementingGenerator{size: 1024}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(ctx))

	cancel()

	time.Sleep(time.Second)

	require.ErrorIs(t, c.Invalidate(), cache.ErrInvalid)
}

// BenchmarkRefreshAheadCacheGet tests single item retrieival performance.
// Expect ~150ns.
func BenchmarkRefreshAheadCacheGet(b *testing.B) {
	b.StopTimer()

	generator := incrementingGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(b, c.Run(b.Context()))

	b.StartTimer()

	for range b.N {
		_, err := c.Get("512")
		require.NoError(b, err)
	}
}

// BenchmarkRefreshAheadCacheGetConcurrent tests single item retrieival performance
// with concurrency.  Expect ~300ns.
func BenchmarkRefreshAheadCacheGetConcurrent(b *testing.B) {
	b.StopTimer()

	generator := incrementingGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(b, c.Run(b.Context()))

	b.StartTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := c.Get("512")
			require.NoError(b, err)
		}
	})
}

// BenchmarkRefreshAheadCacheList tests all item retrieval performance.
// Expect ~15000ns.
func BenchmarkRefreshAheadCacheList(b *testing.B) {
	b.StopTimer()

	generator := incrementingGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(b, c.Run(b.Context()))

	b.StartTimer()

	for range b.N {
		_, err := c.List()
		require.NoError(b, err)
	}
}

// BenchmarkRefreshAheadCacheListConcurrent testes all item retrieval performance
// with concurrency. Expect ~11000ns.
func BenchmarkRefreshAheadCacheListConcurrent(b *testing.B) {
	b.StopTimer()

	generator := incrementingGenerator{size: 1024}

	options := defaultOptions()

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(b, c.Run(b.Context()))

	b.StartTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := c.List()
			require.NoError(b, err)
		}
	})
}
