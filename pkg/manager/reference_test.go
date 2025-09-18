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

package manager_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/manager"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestReferenceGeneration tests that given a client, and an object we generate a
// unique resource reference comprised of the fully qualified resource, group and
// resource name (ID in our case).
func TestReferenceGeneration(t *testing.T) {
	t.Parallel()

	gvk := schema.GroupVersionKind{
		Group:   "networking.k8s.io",
		Version: "v1",
		Kind:    "Ingress",
	}

	restMapper := meta.NewDefaultRESTMapper(nil)
	restMapper.Add(gvk, meta.RESTScopeNamespace)

	client := fake.NewClientBuilder().WithRESTMapper(restMapper).Build()

	object := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}

	reference, err := manager.GetResourceReference(client, object)
	require.NoError(t, err)
	require.Equal(t, "ingresses.networking.k8s.io/foo", reference)
}
