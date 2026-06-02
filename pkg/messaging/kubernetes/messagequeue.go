/*
Copyright 2025 the Unikorn Authors.
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

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/unikorn-cloud/core/pkg/messaging"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	cr "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
)

// MessageQueue implements a message queue like interface using shared informers.
type MessageQueue struct {
	client.Client

	config    *rest.Config
	scheme    *runtime.Scheme
	prototype client.Object
	consumers []messaging.Consumer
}

func New(config *rest.Config, scheme *runtime.Scheme, object client.Object) *MessageQueue {
	return &MessageQueue{
		config:    config,
		scheme:    scheme,
		prototype: object,
	}
}

// NewForManager creates a queue that can be registered with an existing manager.
func NewForManager(object client.Object) *MessageQueue {
	return &MessageQueue{
		prototype: object,
	}
}

var _ = messaging.Queue(&MessageQueue{})

func (q *MessageQueue) Run(ctx context.Context, consumers ...messaging.Consumer) error {
	options := cr.Options{
		// Explicitly adds custom resource support.
		Scheme: q.scheme,
		// Kubernetes doesn't do partitions so we limit to a single consumer.
		LeaderElection:   true,
		LeaderElectionID: filepath.Base(os.Args[0]),
	}

	manager, err := cr.NewManager(q.config, options)
	if err != nil {
		return err
	}

	if err := q.SetupWithManager(manager, consumers...); err != nil {
		return err
	}

	return manager.Start(ctx)
}

// SetupWithManager registers the queue's controller with an existing manager.
func (q *MessageQueue) SetupWithManager(manager crmanager.Manager, consumers ...messaging.Consumer) error {
	q.consumers = consumers
	q.Client = manager.GetClient()

	return cr.NewControllerManagedBy(manager).
		For(q.prototype).
		Complete(q)
}

func (q *MessageQueue) Reconcile(ctx context.Context, request cr.Request) (cr.Result, error) {
	object, ok := q.prototype.DeepCopyObject().(client.Object)
	if !ok {
		return cr.Result{}, fmt.Errorf("%w: prototype copy could not be cast to client.Object", errors.ErrUnsupported)
	}

	if err := q.Get(ctx, request.NamespacedName, object); err != nil {
		if apierrors.IsNotFound(err) {
			return cr.Result{}, nil
		}

		return cr.Result{}, err
	}

	envelope := &messaging.Envelope{
		ResourceID: object.GetName(),
	}

	if t := object.GetDeletionTimestamp(); t != nil {
		envelope.DeletionTimestamp = &t.Time
	}

	for _, consumer := range q.consumers {
		if err := consumer.Consume(ctx, envelope); err != nil {
			return cr.Result{}, err
		}
	}

	return cr.Result{}, nil
}
