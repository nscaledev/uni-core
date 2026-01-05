/*
Copyright 2022-2024 EscherCloud.
Copyright 2024-2025 the Unikorn Authors.
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

package client

import (
	"context"

	argoprojv1 "github.com/unikorn-cloud/core/pkg/apis/argoproj/v1alpha1"
	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"

	"k8s.io/apimachinery/pkg/runtime"
	kubernetesscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SchemeAdder allows custom resources to be added to the scheme.
type SchemeAdder func(*runtime.Scheme) error

// NewScheme returns a scheme with all types that are required by unikorn.
// TODO: we'd really love to include ArgoCD here, but it's dependency hell.
// See https://github.com/argoproj/gitops-engine/issues/56 for a never ending
// commentary on the underlying problem.
func NewScheme(schemes ...SchemeAdder) (*runtime.Scheme, error) {
	// Create a scheme and ensure it knows about Kubernetes and Unikorn
	// resource types.
	scheme := runtime.NewScheme()

	if err := kubernetesscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}

	if err := unikornv1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	if err := unikornv1fake.AddToScheme(scheme); err != nil {
		return nil, err
	}

	if err := argoprojv1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	for _, s := range schemes {
		if err := s(scheme); err != nil {
			return nil, err
		}
	}

	return scheme, nil
}

// New returns a new controller runtime caching client, initialized with core and
// unikorn resources for typed operation.
func New(ctx context.Context, schemes ...SchemeAdder) (client.Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	// Create a scheme and ensure it knows about Kubernetes and Unikorn
	// resource types.
	scheme, err := NewScheme(schemes...)
	if err != nil {
		return nil, err
	}

	cache, err := cache.New(config, cache.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	go func() {
		_ = cache.Start(ctx)
	}()

	clientOptions := client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader:       cache,
			Unstructured: true,
		},
	}

	c, err := client.NewWithWatch(config, clientOptions)
	if err != nil {
		return nil, err
	}

	return c, nil
}
