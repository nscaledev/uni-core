/*
Copyright 2025 the Unikorn Authors.

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

package kubernetes

import (
	"context"
	"os"
	"path/filepath"

	"github.com/unikorn-cloud/core/pkg/messaging"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"

	cr "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MessageQueue implements a message queue like interface using shared informers.
type MessageQueue struct {
	client.Client

	config   *rest.Config
	scheme   *runtime.Scheme
	object   client.Object
	consumer messaging.Consumer
}

func New(config *rest.Config, scheme *runtime.Scheme, object client.Object, consumer messaging.Consumer) *MessageQueue {
	return &MessageQueue{
		config:   config,
		scheme:   scheme,
		object:   object,
		consumer: consumer,
	}
}

var _ = messaging.Queue(&MessageQueue{})

func (q *MessageQueue) Run(ctx context.Context) error {
	options := cr.Options{
		// Explicitly adds custom resource support.
		Scheme: q.scheme,
		// Kubernetes doesn't do partitions so we limit to a single consumer.
		LeaderElection:   true,
		LeaderElectionID: filepath.Base(os.Args[0]),
		// Disables caching, as we don't need the memory overhead.
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				q.object: {
					Transform: toolscache.TransformFunc(func(obj interface{}) (interface{}, error) {
						//nolint: nilnil
						return nil, nil
					}),
				},
			},
		},
	}

	manager, err := cr.NewManager(q.config, options)
	if err != nil {
		return err
	}

	q.Client = manager.GetClient()

	if err := cr.NewControllerManagedBy(manager).For(q.object).Complete(q); err != nil {
		return err
	}

	if err := manager.Start(ctx); err != nil {
		return err
	}

	return nil
}

func (q *MessageQueue) Reconcile(ctx context.Context, request cr.Request) (cr.Result, error) {
	if err := q.Get(ctx, request.NamespacedName, q.object); err != nil {
		if errors.IsNotFound(err) {
			return cr.Result{}, nil
		}

		return cr.Result{}, err
	}

	envelope := &messaging.Envelope{
		ResourceID: q.object.GetName(),
	}

	if t := q.object.GetDeletionTimestamp(); t != nil {
		envelope.DeletionTimestamp = &t.Time
	}

	if err := q.consumer.Consume(ctx, envelope); err != nil {
		return cr.Result{}, err
	}

	return cr.Result{}, nil
}
