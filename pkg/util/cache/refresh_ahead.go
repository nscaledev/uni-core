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

package cache

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	// ErrConflict is raised when our data source returns the same
	// index multiple times.
	ErrConflict = errors.New("a cache index was witnessed more than once")
	// ErrInvalid is raised when there is no cache data e.g. it has
	// not been started.
	ErrInvalid = errors.New("cache invalid")
	// ErrNotFound is returned when the requested index is not in the
	// cache.
	ErrNotFound = errors.New("cache index not found")
	// ErrWorkerPanic is used to handle worker panics.
	ErrWorkerPanic = errors.New("worker panic")
)

// Epoch represents a revision of the cache data.
type Epoch struct {
	epoch time.Time
}

// Valid checks if a previous epoch is still valid against the current one.
func (e Epoch) Valid(previous Epoch) bool {
	return e.epoch.Equal(previous.epoch)
}

// Cacheable defines a cacheable type.
type Cacheable[T any] interface {
	// Index generates an index for the hash table.
	Index() string
	// Equal is used to detect modifications of the data.
	Equal(other *T) bool
}

// CacheablePointer defines a pointer to a type that implements
// the Cacheable interface.
type CacheablePointer[T any] interface {
	*T
	Cacheable[T]
}

// GetSnapshot is a user view of cache data.
type GetSnapshot[T any] struct {
	// Epoch is the revision of the cache data.  The client can memoize any
	// transformations applied to the items and reuse those until a time
	// when the epoch becomes invalid.
	Epoch Epoch
	// Item is the individual cache item.
	Item *T
}

// ListSnapshot is a user view of cache data.
type ListSnapshot[T any] struct {
	// Epoch is the revision of the cache data.  The client can memoize any
	// transformations applied to the items and reuse those until a time
	// when the epoch becomes invalid.
	Epoch Epoch
	// Items are the individual cache items.  No ordering constraints are placed
	// on the snapshot items as this is likely to be more efficient downstream
	// after any potential filtering oprtations.
	Items []*T
}

// RefreshFunc provides the client a way to define how to load the cache data.
// It is recommended that any post processing that happens on raw data also
// occurs during a refresh to hide the cost.
type RefreshFunc[T any, TP CacheablePointer[T]] func(ctx context.Context) ([]TP, error)

// IndexFunc provides the client a way to define how indexes are generated
// from cache resources.  The index must be unique across all resources.
type IndexFunc[T any, TP CacheablePointer[T]] func(t TP) string

// RefreshAheadCacheOptions allows the cache to be configured in various
// ways.
type RefreshAheadCacheOptions struct {
	// RefreshPeriod controls how often to refresh data.
	RefreshPeriod time.Duration
}

const (
	// defaultRefreshPeriod provides a sane default for refreshing.
	// It's assumed that changes that require immediate action will be
	// done with an explicit invalidation.  Changes to the underlying
	// data are assumed to be relatively infrequent.
	defaultRefreshPeriod = time.Hour
)

// cacheMap is the underlying cache implementation.
type cacheMap[T any, TP CacheablePointer[T]] map[string]TP

// Equal returns true if both maps are the same size, contain
// the same keys, and their values are equal.
func (m cacheMap[T, TP]) Equal(o cacheMap[T, TP]) bool {
	if len(m) != len(o) {
		return false
	}

	for k, v := range m {
		ov, ok := o[k]
		if !ok {
			return false
		}

		if !v.Equal(ov) {
			return false
		}
	}

	return true
}

// invalidationRequest allows a client to synchronously trigger
// a cache invalidation.
type invalidationRequest struct {
	// done is closed by the refresh process to indicate completion.
	done chan any
	// err return the refresh error status to the client.
	err error
}

// RefreshAheadCache is a hightly efficient high performance cache
// implementation for sets of resources.
//
// # Synchronization
//
// One of the key observations with a timeout cache that is lazily loaded
// on a cache miss is that someone needs to pay a performance penalty.
// This implementation focuses on background synchronization of data
// either periodically or explicitly through a synchronization operation.
//
// Explicit synchronization is deliberately blocking so that when control
// is returned back to the client, the expected data is guaranteed to be
// in the cache ready for subsequent use.
//
// Either the entire cache can be refreshed, which facilitates addition of
// resources out-of-band, or synchronization of individual resources for
// example on creation or update to avoid having to perform a potentially
// costly rebuild.
//
// # Read Performance
//
// Bacground synchronization ensures every client read will perform equally
// well.  To facilitate efficient lookups of individual resources in the
// cache each resource will be indexed via some form of hashing function
// that uniquely identfies that resource.
//
// The cache features pre-population so it will be ready to use, and this
// synchronization status can be fed directly into Kubernetes readiness
// probes to facilitate a seemless rolling upgrade experience.
//
// # Read Safety
//
// If we naively passed a slice up to the client, then that could be
// accidentally mutated through filtering operations such as slices.Delete.
// The easy fix is to perform a deep copy of the data, but this is costly
// due to the use of runtime reflection.
//
// We implement a halfway policy that is both performant and also less
// likely to fall foul of accidental mutation.  Cache resources are only
// ever referred by pointer, facilitating zero copy for individual resources.
// When a client wishes to list all resources we return a new array that
// can be mutated pointing to each of the resources, doing only the minimal
// amount of work as is necessary.
//
// # Downstream Performance Optimization
//
// Synchronization events are epochs.  When a client lists resources, not
// only does it return an array of resource pointers, it also returns the
// epoch for which it is valid for.
//
// This allows clients to memoize any transformations made against the
// cached resurces and further improve performance.  An example of this
// is JSON encoding which uses runtime type reflection and is relatively
// costly.
type RefreshAheadCache[T any, TP CacheablePointer[T]] struct {
	// options provide cache configuration.
	options *RefreshAheadCacheOptions
	// epoch that the cache is valid for.
	epoch Epoch
	// refresh is used to refresh the entire cache in the background.
	refresh RefreshFunc[T, TP]
	// cache records the actual data.
	cache cacheMap[T, TP]
	// lock controls concurrent accesses.
	lock sync.RWMutex
	// invalidations is a channel that allows a client to synchronously
	// perform a refresh, useful for situations where you need a value
	// to be visible in the cache before continuation.
	invalidations chan *invalidationRequest

	// pendingLock guards pending.
	pendingLock sync.Mutex
	// pending is the in-flight invalidation request, if any.  Concurrent
	// callers coalesce onto this rather than each queuing a separate refresh.
	pending *invalidationRequest
}

// NewRefreshAheadCache constructs a new refresh ahead cache.
func NewRefreshAheadCache[T any, TP CacheablePointer[T]](refresh RefreshFunc[T, TP], options *RefreshAheadCacheOptions) *RefreshAheadCache[T, TP] {
	return &RefreshAheadCache[T, TP]{
		refresh: refresh,
		options: options,
	}
}

// Run performs a synchronous refresh to pre load cache data and
// starts the background refresher.
func (c *RefreshAheadCache[T, TP]) Run(ctx context.Context) error {
	if err := c.doRefresh(ctx); err != nil {
		return err
	}

	c.invalidations = make(chan *invalidationRequest)

	refresher := func() {
		refreshPeriod := defaultRefreshPeriod

		if c.options.RefreshPeriod != 0 {
			refreshPeriod = c.options.RefreshPeriod
		}

		ticker := time.NewTicker(refreshPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				close(c.invalidations)
				return
			case request := <-c.invalidations:
				// This request is about to be attempted. Clear the pending field so that the next
				// caller of Invalidate will create their own pending request, and not glom
				// onto this one while it's in flight.
				c.pendingLock.Lock()
				c.pending = nil
				c.pendingLock.Unlock()

				request.err = c.doRefresh(ctx)
				close(request.done)
			case <-ticker.C:
				if err := c.doRefresh(ctx); err != nil {
					log.Log.Error(err, "failed to refresh cache data")
				}
			}
		}
	}

	go refresher()

	return nil
}

// Invalidate performs a synchronous invalidation of the cache and only
// returns control to the client when the refresh has completed, guaranteeing
// on success that the cache will contain any new values.
func (c *RefreshAheadCache[T, TP]) Invalidate() error {
	c.pendingLock.Lock()

	// Concurrent callers coalesce: if a refresh is already waiting, the caller will
	// wait for the same refresh and receive its result, rather than blocking on its
	// own refresh.
	if c.pending != nil {
		req := c.pending
		c.pendingLock.Unlock()

		<-req.done

		return req.err
	}

	// We are the designated sender for this round.
	request := &invalidationRequest{
		done: make(chan any),
	}

	c.pending = request
	c.pendingLock.Unlock()

	// sendInvalidation handles the send with panic recovery so that if the
	// channel has been closed by a shutdown it cleans up correctly and
	// unblocks any callers already waiting on request.done.
	if err := c.sendInvalidation(request); err != nil {
		return err
	}

	<-request.done

	return request.err
}

// sendInvalidation sends request to the refresh goroutine.  If the channel
// has been closed (cache shutdown) the resulting panic is recovered, pending
// is cleared, and any goroutines already waiting on request.done are
// unblocked with ErrInvalid.
func (c *RefreshAheadCache[T, TP]) sendInvalidation(request *invalidationRequest) (err error) {
	defer func() {
		if x := recover(); x != nil {
			request.err = ErrInvalid

			c.pendingLock.Lock()
			c.pending = nil
			c.pendingLock.Unlock()

			close(request.done)

			err = ErrInvalid
		}
	}()

	// NOTE: callers will block here until the channel is initialized by Run().
	c.invalidations <- request

	return nil
}

// Get does a zero copy read of a specified item.
func (c *RefreshAheadCache[T, TP]) Get(index string) (*GetSnapshot[T], error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.cache == nil {
		return nil, ErrInvalid
	}

	item, ok := c.cache[index]
	if !ok {
		return nil, fmt.Errorf("%w: requested index %s", ErrNotFound, index)
	}

	result := &GetSnapshot[T]{
		Epoch: c.epoch,
		Item:  item,
	}

	return result, nil
}

// List does a zero copy read of all items.
func (c *RefreshAheadCache[T, TP]) List() (*ListSnapshot[T], error) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	if c.cache == nil {
		return nil, ErrInvalid
	}

	items := make([]*T, len(c.cache))

	i := 0

	for item := range maps.Values(c.cache) {
		items[i] = item
		i++
	}

	result := &ListSnapshot[T]{
		Epoch: c.epoch,
		Items: items,
	}

	return result, nil
}

// doRefresh does a refresh of all cache data.
func (c *RefreshAheadCache[T, TP]) doRefresh(ctx context.Context) error {
	// Ensure the refresh routine cannot ever crash.
	defer func() {
		if x := recover(); x != nil {
			log.Log.Error(ErrWorkerPanic, "caught unhandled exception", "value", x)
		}
	}()

	// Collect the refreshed data.
	data, err := c.refresh(ctx)
	if err != nil {
		return err
	}

	cache := make(cacheMap[T, TP], len(data))

	for i := range data {
		index := data[i].Index()

		if _, ok := cache[index]; ok {
			return fmt.Errorf("%w: offending key %s", ErrConflict, index)
		}

		cache[index] = data[i]
	}

	// Has anything actually changed?
	// NOTE: this is performed unlocked as this function is the only thing
	// that can modify the cache.
	if cache.Equal(c.cache) {
		return nil
	}

	// Write the new data.
	c.lock.Lock()
	defer c.lock.Unlock()

	c.epoch.epoch = time.Now()
	c.cache = cache

	return nil
}
