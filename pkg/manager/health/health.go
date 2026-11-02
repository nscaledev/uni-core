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

// Package health gives controller-runtime managers built by core a health
// probe endpoint, so a chart can tell a running manager from one that has
// started and is about to exit.
package health

import (
	"context"
	"errors"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Address is where the manager serves /healthz and /readyz. It sits beside
// the metrics endpoint on :8080, as in kubebuilder's scaffolding.
const Address = ":8081"

// ErrCacheNotSynced is returned by the readiness check until the manager's
// informer caches have synced.
var ErrCacheNotSynced = errors.New("informer caches not synced")

// Options returns the manager options core uses: a scheme, leader election
// under the given lease ID, and health probes on Address.
func Options(scheme *runtime.Scheme, leaderElectionID string) manager.Options {
	return manager.Options{
		Scheme:                 scheme,
		LeaderElection:         true,
		LeaderElectionID:       leaderElectionID,
		HealthProbeBindAddress: Address,
	}
}

// CacheSyncer is the part of a manager's cache the readiness check uses.
type CacheSyncer interface {
	WaitForCacheSync(ctx context.Context) bool
}

// CacheSynced reports ready once the cache has synced. A probe that times
// out before then cancels the request context, which fails the check.
func CacheSynced(cache CacheSyncer) healthz.Checker {
	return func(req *http.Request) error {
		if !cache.WaitForCacheSync(req.Context()) {
			return ErrCacheNotSynced
		}

		return nil
	}
}

// AddChecks registers a liveness ping on /healthz and a cache sync check on
// /readyz.
func AddChecks(mgr manager.Manager) error {
	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		return err
	}

	return mgr.AddReadyzCheck("cache-sync", CacheSynced(mgr.GetCache()))
}
