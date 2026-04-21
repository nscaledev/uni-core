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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type internalOverlayType struct {
	id     string
	status string
}

func (t *internalOverlayType) Index() string {
	return t.id
}

func (t *internalOverlayType) Equal(o *internalOverlayType) bool {
	return *t == *o
}

// This test must live in the internal package because it verifies the private
// overlay state directly. The external cache_test package covers the same race
// at the public API boundary, but only the internal package can assert that the
// overlay entry itself survives the in-flight refresh and is then discarded by
// the next refresh.
func TestOverlaySurvivesInFlightRefreshOnly(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		items   = []*internalOverlayType{{id: "image", status: "ready"}}
		started = make(chan struct{})
		proceed = make(chan struct{})
		block   atomic.Bool
		once    sync.Once
	)

	refresh := func(_ context.Context) ([]*internalOverlayType, error) {
		if block.Load() {
			once.Do(func() { close(started) })

			<-proceed
		}

		lock.Lock()
		defer lock.Unlock()

		current := make([]*internalOverlayType, len(items))
		copy(current, items)

		return current, nil
	}

	options := &RefreshAheadCacheOptions{
		RefreshPeriod: time.Minute,
	}

	c := NewRefreshAheadCache[internalOverlayType](refresh, options)
	require.NoError(t, c.Run(t.Context()))

	block.Store(true)

	done := make(chan error, 1)

	go func() {
		done <- c.Invalidate()
	}()

	<-started

	lock.Lock()
	items = []*internalOverlayType{{id: "image", status: "delete_pending"}}
	lock.Unlock()

	require.NoError(t, c.Upsert(&internalOverlayType{id: "image", status: "delete_pending"}))

	close(proceed)
	require.NoError(t, <-done)

	c.lock.RLock()
	require.Len(t, c.overlay, 1)
	c.lock.RUnlock()

	block.Store(false)

	require.NoError(t, c.Invalidate())

	c.lock.RLock()
	require.Empty(t, c.overlay)
	c.lock.RUnlock()
}
