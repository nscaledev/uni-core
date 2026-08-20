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

package client //nolint:testpackage // Tests exercise the private cache lifecycle helper.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeCacheRunner struct {
	start            func(context.Context) error
	waitForCacheSync func(context.Context) bool
}

var errStartFailed = errors.New("start failed")

func requireClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, "cache lifecycle context was not canceled")
	}
}

func (f *fakeCacheRunner) Start(ctx context.Context) error {
	return f.start(ctx)
}

func (f *fakeCacheRunner) WaitForCacheSync(ctx context.Context) bool {
	return f.waitForCacheSync(ctx)
}

func TestStartAndSyncCacheWaitsForSynchronization(t *testing.T) {
	t.Parallel()

	stop := make(chan struct{})
	synchronize := make(chan struct{})
	startCalled := make(chan struct{})
	waitCalled := make(chan struct{})

	runner := &fakeCacheRunner{
		start: func(context.Context) error {
			close(startCalled)
			<-stop

			return nil
		},
		waitForCacheSync: func(context.Context) bool {
			close(waitCalled)
			<-synchronize

			return true
		},
	}

	result := make(chan error, 1)

	go func() {
		result <- startAndSyncCache(t.Context(), runner)
	}()

	<-startCalled
	<-waitCalled

	select {
	case err := <-result:
		require.Failf(t, "cache startup returned before synchronization", "error: %v", err)
	default:
	}

	close(synchronize)
	require.NoError(t, <-result)
	close(stop)
}

func TestStartAndSyncCacheReturnsStartError(t *testing.T) {
	t.Parallel()

	waitStopped := make(chan struct{})
	runner := &fakeCacheRunner{
		start: func(context.Context) error {
			return errStartFailed
		},
		waitForCacheSync: func(ctx context.Context) bool {
			<-ctx.Done()
			close(waitStopped)

			return false
		},
	}

	err := startAndSyncCache(t.Context(), runner)

	require.ErrorIs(t, err, errStartFailed)
	require.ErrorContains(t, err, "start cache")
	requireClosed(t, waitStopped)
}

func TestStartAndSyncCacheRejectsPrematureStop(t *testing.T) {
	t.Parallel()

	waitRelease := make(chan struct{})
	runner := &fakeCacheRunner{
		start: func(context.Context) error {
			return nil
		},
		waitForCacheSync: func(context.Context) bool {
			<-waitRelease

			return false
		},
	}

	err := startAndSyncCache(t.Context(), runner)

	close(waitRelease)

	require.EqualError(t, err, "cache stopped before synchronization")
}

func TestStartAndSyncCacheReturnsSyncFailure(t *testing.T) {
	t.Parallel()

	startStopped := make(chan struct{})
	runner := &fakeCacheRunner{
		start: func(ctx context.Context) error {
			<-ctx.Done()
			close(startStopped)

			return nil
		},
		waitForCacheSync: func(context.Context) bool {
			return false
		},
	}

	err := startAndSyncCache(t.Context(), runner)

	require.EqualError(t, err, "cache failed to synchronize")
	requireClosed(t, startStopped)
}

func TestStartAndSyncCachePreservesContextCancellationWhenCacheStops(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	waitRelease := make(chan struct{})
	runner := &fakeCacheRunner{
		start: func(context.Context) error {
			return nil
		},
		waitForCacheSync: func(context.Context) bool {
			<-waitRelease
			return false
		},
	}

	err := startAndSyncCache(ctx, runner)

	close(waitRelease)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "synchronize cache")
}

func TestStartAndSyncCacheRejectsCanceledContextAfterSynchronization(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	startRelease := make(chan struct{})
	startStopped := make(chan struct{})
	runner := &fakeCacheRunner{
		start: func(context.Context) error {
			<-startRelease
			close(startStopped)

			return nil
		},
		waitForCacheSync: func(context.Context) bool {
			return true
		},
	}

	err := startAndSyncCache(ctx, runner)

	close(startRelease)

	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "synchronize cache")
	requireClosed(t, startStopped)
}
