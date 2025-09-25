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

	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"
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

	reference, err := manager.GenerateResourceReference(client, object)
	require.NoError(t, err)
	require.Equal(t, "ingresses.networking.k8s.io/foo", reference)
}

// TestRererenceRead tests reference reads work correctly, in particular the
// filtering out of any well-known non-reference finalizers, but also ensuring
// the operation treats the underlying object as immutable (slices.Delete does
// things you may not expect to the underlying array of a slice...)
func TestRererenceRead(t *testing.T) {
	t.Parallel()

	reference1 := "cat"
	reference2 := "dag"

	object := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
			Finalizers: []string{
				constants.Finalizer,
				reference1,
				reference2,
			},
		},
	}

	references := manager.GetResourceReferences(object)
	require.Len(t, references, 2)
	require.Contains(t, references, reference1)
	require.Contains(t, references, reference2)

	require.Len(t, object.Finalizers, 3)
	require.Contains(t, object.Finalizers, constants.Finalizer)
	require.Contains(t, object.Finalizers, reference1)
	require.Contains(t, object.Finalizers, reference2)
}

// TestReferenceClear tests references can be cleared in bulk from a collection
// of resources.
func TestReferenceClear(t *testing.T) {
	t.Parallel()

	reference := "cat"
	label := "animal"
	labelValue1 := "giraffe"
	labelValue2 := "elephant"
	namespace1 := "donkey"
	namespace2 := "dragon"

	objects := &networkingv1.IngressList{
		Items: []networkingv1.Ingress{
			// Reference removed, finalizer intact.
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace1,
					Name:      "foo",
					Labels: map[string]string{
						label: labelValue1,
					},
					Finalizers: []string{
						constants.Finalizer,
						reference,
					},
				},
			},
			// Nothing happens and it doesn't crash.
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace1,
					Name:      "bar",
					Labels: map[string]string{
						label: labelValue1,
					},
				},
			},
			// Unaffected as part of a different selection.
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace1,
					Name:      "baz",
					Labels: map[string]string{
						label: labelValue2,
					},
					Finalizers: []string{
						constants.Finalizer,
						reference,
					},
				},
			},
			// Unaffected as part of a different namespace.
			{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace2,
					Name:      "baz",
					Labels: map[string]string{
						label: labelValue2,
					},
					Finalizers: []string{
						constants.Finalizer,
						reference,
					},
				},
			},
		},
	}

	o := []client.Object{
		&objects.Items[0],
		&objects.Items[1],
		&objects.Items[2],
		&objects.Items[3],
	}

	cli := fake.NewClientBuilder().WithObjects(o...).Build()

	options := &client.ListOptions{
		Namespace: namespace1,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			label: labelValue1,
		}),
	}

	require.NoError(t, manager.ClearResourceReferences(t.Context(), cli, &networkingv1.IngressList{}, options, reference))

	object := &networkingv1.Ingress{}

	require.NoError(t, cli.Get(t.Context(), client.ObjectKey{Namespace: namespace1, Name: "foo"}, object))
	require.Len(t, object.Finalizers, 1)
	require.Contains(t, object.Finalizers, constants.Finalizer)

	require.NoError(t, cli.Get(t.Context(), client.ObjectKey{Namespace: namespace1, Name: "bar"}, object))
	require.Len(t, object.Finalizers, 0)

	require.NoError(t, cli.Get(t.Context(), client.ObjectKey{Namespace: namespace1, Name: "baz"}, object))
	require.Len(t, object.Finalizers, 2)
	require.Contains(t, object.Finalizers, constants.Finalizer)
	require.Contains(t, object.Finalizers, reference)

	require.NoError(t, cli.Get(t.Context(), client.ObjectKey{Namespace: namespace2, Name: "baz"}, object))
	require.Len(t, object.Finalizers, 2)
	require.Contains(t, object.Finalizers, constants.Finalizer)
	require.Contains(t, object.Finalizers, reference)
}
