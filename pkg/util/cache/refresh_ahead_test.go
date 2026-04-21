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
	"sync"
	"sync/atomic"
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

type overlayType struct {
	id     string
	status string
}

func (t *overlayType) Index() string {
	return t.id
}

func (t *overlayType) Equal(o *overlayType) bool {
	return *t == *o
}

type overlayGenerator struct {
	lock  sync.Mutex
	items []*overlayType
}

func (g *overlayGenerator) set(items ...*overlayType) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.items = items
}

func (g *overlayGenerator) refresh(_ context.Context) ([]*overlayType, error) {
	g.lock.Lock()
	defer g.lock.Unlock()

	items := make([]*overlayType, len(g.items))
	copy(items, g.items)

	return items, nil
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

// TestConcurrentInvalidation tests that concurrent callers all unblock without
// error, and that coalescing ensures significantly fewer refreshes than callers.
func TestConcurrentInvalidation(t *testing.T) {
	t.Parallel()

	generator := &incrementingGenerator{size: 16}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[myType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	initialGeneration := generator.generation

	const n = 10

	// Release all goroutines simultaneously to maximise coalescing.
	start := make(chan struct{})

	results := make([]error, n)

	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			results[i] = c.Invalidate()
		}()
	}

	close(start)
	wg.Wait()

	for _, err := range results {
		require.NoError(t, err)
	}

	// Each refresh takes ~1s (incrementingGenerator delay).  With coalescing,
	// N concurrent callers should trigger far fewer than N refreshes.
	refreshes := generator.generation - initialGeneration
	require.Less(t, refreshes, n)
}

// TestInvalidationFreshness tests that every caller of Invalidate sees cache
// data that was generated after their call, not from a refresh that was already
// in progress when they called it.
func TestInvalidationFreshness(t *testing.T) {
	t.Parallel()

	var gen atomic.Int32

	// started is closed by the first non-warmup refresh once it has incremented
	// the generation counter, signalling that it is in progress.
	started := make(chan struct{})
	// proceed is closed by the test to release the blocked refresh.
	proceed := make(chan struct{})

	refresh := func(_ context.Context) ([]*myType, error) {
		n := int(gen.Add(1)) - 1

		items := make([]*myType, 16)
		for i := range items {
			items[i] = &myType{id: n + i}
		}

		// Warmup call (n==0) runs freely; subsequent calls block until the
		// test releases them, giving other goroutines time to arrive.
		if n > 0 {
			select {
			case <-started:
			default:
				close(started)
			}

			<-proceed
		}

		return items, nil
	}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[myType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	// Trigger a refresh and let it block inside the refresh function.
	go func() { _ = c.Invalidate() }()

	// Wait until the refresh is in progress and the generation counter has
	// already been incremented.
	<-started

	// These goroutines arrive while the refresh is in flight.  Each records
	// the current generation as the minimum id its result must satisfy.
	const n = 5

	type result struct {
		minIDAtCall int
		snapshot    *cache.ListSnapshot[myType]
		err         error
	}

	results := make([]result, n)

	var wg sync.WaitGroup

	for i := range n {
		wg.Add(1)

		go func() {
			defer wg.Done()

			minID := int(gen.Load())

			if err := c.Invalidate(); err != nil {
				results[i].err = err
				return
			}

			snapshot, err := c.List()
			results[i] = result{minIDAtCall: minID, snapshot: snapshot, err: err}
		}()
	}

	// Release the blocked refresh and wait for all callers to return.
	close(proceed)
	wg.Wait()

	for _, r := range results {
		require.NoError(t, r.err)

		for _, item := range r.snapshot.Items {
			require.GreaterOrEqual(t, item.id, r.minIDAtCall,
				"cache item predates the Invalidate call")
		}
	}
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

func TestInsertIfAbsentYieldsToNextRefreshWhenBackendOmitsKey(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "base", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "new", status: "creating"}))

	generator.set(&overlayType{id: "base", status: "ready"})

	require.NoError(t, c.Invalidate())

	_, err := c.Get("new")
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestUpsertYieldsToNextRefreshWhenBackendChangesKey(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "image", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}))

	generator.set(&overlayType{id: "image", status: "ready"})

	require.NoError(t, c.Invalidate())

	updated, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "ready", updated.Item.status)
}

func TestInsertIfAbsentDoesNothingWhenKeyAlreadyVisible(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "image", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	before, err := c.Get("image")
	require.NoError(t, err)

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "image", status: "creating"}))

	after, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "ready", after.Item.status)
	require.True(t, after.Epoch.Valid(before.Epoch))
}

func TestInsertIfAbsentDoesNothingWhenLiveOverlayAlreadyExists(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "base", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}))

	before, err := c.Get("image")
	require.NoError(t, err)

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "image", status: "creating"}))

	after, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", after.Item.status)
	require.True(t, after.Epoch.Valid(before.Epoch))
}

func TestUpsertInsertsWhenKeyMissing(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "base", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.Upsert(&overlayType{id: "missing", status: "delete_pending"}))

	item, err := c.Get("missing")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", item.Item.status)
}

func TestInsertIfAbsentErrorsBeforeRun(t *testing.T) {
	t.Parallel()

	c := cache.NewRefreshAheadCache[overlayType](func(context.Context) ([]*overlayType, error) {
		return nil, nil
	}, &cache.RefreshAheadCacheOptions{})

	require.ErrorIs(t, c.InsertIfAbsent(&overlayType{id: "image", status: "creating"}), cache.ErrInvalid)
}

func TestUpsertErrorsBeforeRun(t *testing.T) {
	t.Parallel()

	c := cache.NewRefreshAheadCache[overlayType](func(context.Context) ([]*overlayType, error) {
		return nil, nil
	}, &cache.RefreshAheadCacheOptions{})

	require.ErrorIs(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}), cache.ErrInvalid)
}

func TestInsertIfAbsentYieldsToNextRefreshWhenBackendContainsKey(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(&overlayType{id: "base", status: "ready"})

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "new", status: "creating"}))

	generator.set(
		&overlayType{id: "base", status: "ready"},
		&overlayType{id: "new", status: "ready"},
	)

	require.NoError(t, c.Invalidate())

	item, err := c.Get("new")
	require.NoError(t, err)
	require.Equal(t, "ready", item.Item.status)
}

func TestInsertIfAbsentYieldsToNextRefreshWhenBackendDropsKey(t *testing.T) {
	t.Parallel()

	generator := &overlayGenerator{}
	generator.set(
		&overlayType{id: "base", status: "ready"},
		&overlayType{id: "new", status: "ready"},
	)

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](generator.refresh, options)
	require.NoError(t, c.Run(t.Context()))

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "new", status: "creating"}))

	generator.set(&overlayType{id: "base", status: "ready"})

	require.NoError(t, c.Invalidate())

	_, err := c.Get("new")
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestConcurrentWritePreservesEpochAcrossStaleRefresh(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		items   = []*overlayType{{id: "image", status: "ready"}}
		started = make(chan struct{})
		proceed = make(chan struct{})
		block   atomic.Bool
		once    sync.Once
	)

	refresh := func(_ context.Context) ([]*overlayType, error) {
		lock.Lock()
		current := make([]*overlayType, len(items))
		copy(current, items)
		lock.Unlock()

		if block.Load() {
			// Capture the stale backend snapshot first, then block. The concurrent
			// Upsert that follows should therefore outrank this refresh and remain
			// visible until a later refresh starts.
			once.Do(func() { close(started) })

			<-proceed
		}

		return current, nil
	}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	block.Store(true)

	done := make(chan error, 1)

	go func() {
		done <- c.Invalidate()
	}()

	<-started

	require.NoError(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}))

	writeSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", writeSnapshot.Item.status)

	close(proceed)
	require.NoError(t, <-done)

	finalSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", finalSnapshot.Item.status)
	require.True(t, finalSnapshot.Epoch.Valid(writeSnapshot.Epoch))

	require.NoError(t, c.Invalidate())

	refreshedSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "ready", refreshedSnapshot.Item.status)
	require.False(t, refreshedSnapshot.Epoch.Valid(finalSnapshot.Epoch))
}

func TestConcurrentInsertIfAbsentPreservesEpochAcrossStaleRefresh(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		items   = []*overlayType{{id: "base", status: "ready"}}
		started = make(chan struct{})
		proceed = make(chan struct{})
		block   atomic.Bool
		once    sync.Once
	)

	refresh := func(_ context.Context) ([]*overlayType, error) {
		lock.Lock()
		current := make([]*overlayType, len(items))
		copy(current, items)
		lock.Unlock()

		if block.Load() {
			once.Do(func() { close(started) })

			<-proceed
		}

		return current, nil
	}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	block.Store(true)

	done := make(chan error, 1)

	go func() {
		done <- c.Invalidate()
	}()

	<-started

	require.NoError(t, c.InsertIfAbsent(&overlayType{id: "image", status: "creating"}))

	writeSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "creating", writeSnapshot.Item.status)

	close(proceed)
	require.NoError(t, <-done)

	finalSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "creating", finalSnapshot.Item.status)
	require.True(t, finalSnapshot.Epoch.Valid(writeSnapshot.Epoch))

	lock.Lock()
	items = []*overlayType{{id: "base", status: "ready"}, {id: "image", status: "ready"}}
	lock.Unlock()

	require.NoError(t, c.Invalidate())

	refreshedSnapshot, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "ready", refreshedSnapshot.Item.status)
	require.False(t, refreshedSnapshot.Epoch.Valid(finalSnapshot.Epoch))
}

func TestRefreshWithSurvivingOverlayBumpsEpochWhenVisibleViewChanges(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		items   = []*overlayType{{id: "image", status: "ready"}, {id: "other", status: "ready"}}
		started = make(chan struct{})
		proceed = make(chan struct{})
		block   atomic.Bool
		once    sync.Once
	)

	refresh := func(_ context.Context) ([]*overlayType, error) {
		if block.Load() {
			once.Do(func() { close(started) })

			<-proceed
		}

		lock.Lock()
		current := make([]*overlayType, len(items))
		copy(current, items)
		lock.Unlock()

		return current, nil
	}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	block.Store(true)

	done := make(chan error, 1)

	go func() {
		done <- c.Invalidate()
	}()

	<-started

	lock.Lock()
	items = []*overlayType{{id: "image", status: "ready"}, {id: "other", status: "updated"}}
	lock.Unlock()

	require.NoError(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}))

	before, err := c.List()
	require.NoError(t, err)

	close(proceed)
	require.NoError(t, <-done)

	after, err := c.List()
	require.NoError(t, err)
	require.False(t, after.Epoch.Valid(before.Epoch))

	image, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", image.Item.status)

	other, err := c.Get("other")
	require.NoError(t, err)
	require.Equal(t, "updated", other.Item.status)
}

func TestRefreshWithSurvivingOverlayKeepsEpochWhenVisibleViewIsUnchanged(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		items   = []*overlayType{{id: "image", status: "ready"}}
		started = make(chan struct{})
		proceed = make(chan struct{})
		block   atomic.Bool
		once    sync.Once
	)

	refresh := func(_ context.Context) ([]*overlayType, error) {
		lock.Lock()
		current := make([]*overlayType, len(items))
		copy(current, items)
		lock.Unlock()

		if block.Load() {
			once.Do(func() { close(started) })

			<-proceed
		}

		return current, nil
	}

	options := &cache.RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := cache.NewRefreshAheadCache[overlayType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	block.Store(true)

	done := make(chan error, 1)

	go func() {
		done <- c.Invalidate()
	}()

	<-started

	require.NoError(t, c.Upsert(&overlayType{id: "image", status: "delete_pending"}))

	before, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", before.Item.status)

	close(proceed)
	require.NoError(t, <-done)

	after, err := c.Get("image")
	require.NoError(t, err)
	require.Equal(t, "delete_pending", after.Item.status)
	require.True(t, after.Epoch.Valid(before.Epoch))
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
