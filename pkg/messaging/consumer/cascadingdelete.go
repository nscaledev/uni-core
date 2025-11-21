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

package consumer

import (
	"context"
	"fmt"

	"github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/messaging"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CascadingDelete implements a message queue consumer that watches
// for resource deletion events and then fans them out to another local type.
// You almost certainly want a resource label to be defined corrsponding to
// the messages's resource ID, or you will just delete everything.
type CascadingDelete struct {
	// client is a Kubernetes client.
	client client.Client
	// namespace is the where to look for resources.
	namespace string
	// resourceLabel if set defines a label to use for resource selection
	// based on the resource ID passed in the message envelope.
	resourceLabel string
	// resources is storage for resources being searched for.
	resources client.ObjectList
}

var _ = messaging.Consumer(&CascadingDelete{})

// Options defines a set of runtime composable options.
type Option func(c *CascadingDelete)

// WithNamespace sets the namespace in which to each for resources.
func WithNamespace(namespace string) Option {
	return func(c *CascadingDelete) {
		c.namespace = namespace
	}
}

// WithResourceLabel creates a label selector that will match the value passed
// in the messages's resource ID.
func WithResourceLabel(label string) Option {
	return func(c *CascadingDelete) {
		c.resourceLabel = label
	}
}

// NewCascadingDelete creates a new cascading deletion consumer.
func NewCascadingDelete(client client.Client, resources client.ObjectList, options ...Option) *CascadingDelete {
	c := &CascadingDelete{
		client:    client,
		resources: resources,
	}

	for _, o := range options {
		o(c)
	}

	return c
}

// Consume receives project events.  If the resource is being deleted, we propagate
// that deletion request to all local resources that reference that resource.
func (c *CascadingDelete) Consume(ctx context.Context, envelope *messaging.Envelope) error {
	log := log.FromContext(ctx)

	if envelope.DeletionTimestamp == nil {
		log.V(1).Info("ignoring live resource", "id", envelope.ResourceID)
		return nil
	}

	opts := &client.ListOptions{
		Namespace: c.namespace,
	}

	if c.resourceLabel != "" {
		opts.LabelSelector = labels.SelectorFromSet(labels.Set{
			c.resourceLabel: envelope.ResourceID,
		})
	}

	if err := c.client.List(ctx, c.resources, opts); err != nil {
		return err
	}

	// Some resources may use ownder references to perform cascading deletion
	// of their children, and they will need to block until cleanup has occurred.
	deleteOptions := &client.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationForeground),
	}

	deleteItem := func(object runtime.Object) error {
		resource, ok := object.(client.Object)
		if !ok {
			return fmt.Errorf("%w: cannot convert from runtime object to client", errors.ErrTypeConversion)
		}

		if resource.GetDeletionTimestamp() != nil {
			log.V(1).Info("awaiting resource deletion", "id", resource.GetName())
			return nil
		}

		log.Info("deleting resource", "id", resource.GetName())

		if err := c.client.Delete(ctx, resource, deleteOptions); err != nil {
			return err
		}

		return nil
	}

	// This is literally the best thing ever!
	// Well, sort of, it almost definitely uses reflection...
	return meta.EachListItem(c.resources, deleteItem)
}
