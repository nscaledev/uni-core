/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.

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

package client

import (
	"context"

	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterContext is available to all provisioners and is updated when invoked
// within the scope of a remote cluster provisioner.  In general, you should avoid
// having to use this, because it implies hackery.
type ClusterContext struct {
	// Client is a Kubernetes client that can access resources on the remote
	// cluster.  This is typically used to extract Kubenetes configuration to
	// chain to the next remote cluster.  It is also used when some combination
	// of Helm application/ArgoCD is broken and needs manual intervention.
	Client client.Client
	// ID is the unique remote cluster ID as cunsumed by the CD layer.
	// This is only set on invocation of a remote cluster provisioner.
	ID *cd.ResourceIdentifier
	// Host is the Kubernetes endpoint hostname. This is only set on
	// invocation of a remote cluster provisioner.
	Host string
	// Port is the Kubernetes endpoint port. This is only set on
	// invocation of a remote cluster provisioner.
	Port string
}

type key int

const (
	// provisionerClientKey is the client that is scoped to the cluster containing
	// the current provisioner (i.e. continuous deployment layer).
	provisionerClientKey key = iota

	// clusterKey sets the cluster so it's propagated to all
	// descendant provisioners in the call graph.
	clusterKey

	// namespaceKey is used to propagate the process's namespace to clients.
	namespaceKey
)

func NewContext(ctx context.Context, client client.Client) context.Context {
	return context.WithValue(ctx, provisionerClientKey, client)
}

func FromContext(ctx context.Context) (client.Client, error) {
	if value := ctx.Value(provisionerClientKey); value != nil {
		if client, ok := value.(client.Client); ok {
			return client, nil
		}
	}

	return nil, errors.ErrInvalidContext
}

func NewContextWithCluster(ctx context.Context, remote *ClusterContext) context.Context {
	return context.WithValue(ctx, clusterKey, remote)
}

func ClusterFromContext(ctx context.Context) (*ClusterContext, error) {
	if value := ctx.Value(clusterKey); value != nil {
		if remote, ok := value.(*ClusterContext); ok {
			return remote, nil
		}
	}

	return nil, errors.ErrInvalidContext
}

func NewContextWithNamespace(ctx context.Context, namespace string) context.Context {
	return context.WithValue(ctx, namespaceKey, namespace)
}

func NamespaceFromContext(ctx context.Context) (string, error) {
	if value := ctx.Value(namespaceKey); value != nil {
		if namespace, ok := value.(string); ok {
			return namespace, nil
		}
	}

	return "", errors.ErrInvalidContext
}
