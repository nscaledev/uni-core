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

package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/manager/health"
)

type fakeCache struct {
	synced bool
}

func (c fakeCache) WaitForCacheSync(context.Context) bool {
	return c.synced
}

func TestCacheSynced(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	require.NoError(t, health.CacheSynced(fakeCache{synced: true})(req))
	require.ErrorIs(t, health.CacheSynced(fakeCache{synced: false})(req), health.ErrCacheNotSynced)
}

func TestOptions(t *testing.T) {
	t.Parallel()

	options := health.Options(nil, "lease")

	require.True(t, options.LeaderElection)
	require.Equal(t, "lease", options.LeaderElectionID)
	require.Equal(t, health.Address, options.HealthProbeBindAddress)
}
