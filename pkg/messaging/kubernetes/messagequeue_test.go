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

package kubernetes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/unikorn-cloud/core/pkg/messaging/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	cr "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var errClientFailed = errors.New("client failed")

type errorClient struct {
	client.Client
	err error
}

func (c *errorClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

func mustNewScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()

	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	return scheme
}

func newMessageQueue(t *testing.T, objects ...client.Object) *kubernetes.MessageQueue {
	t.Helper()

	scheme := mustNewScheme(t)

	q := kubernetes.New(nil, scheme, &corev1.ConfigMap{})
	q.Client = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	return q
}

func TestReconcileDoesNotMutatePrototype(t *testing.T) {
	t.Parallel()

	const name = "resource"

	scheme := mustNewScheme(t)
	prototype := &corev1.ConfigMap{}
	q := kubernetes.New(nil, scheme, prototype)
	q.Client = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: metav1.NamespaceDefault,
			},
		}).
		Build()

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if got := prototype.GetName(); got != "" {
		t.Fatalf("expected prototype name to remain empty, got %q", got)
	}
}

func TestReconcileCanReusePrototypeForMultipleObjects(t *testing.T) {
	t.Parallel()

	const (
		first  = "first"
		second = "second"
	)

	scheme := mustNewScheme(t)
	prototype := &corev1.ConfigMap{}
	q := kubernetes.New(nil, scheme, prototype)
	q.Client = fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      first,
				Namespace: metav1.NamespaceDefault,
			},
		}, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      second,
				Namespace: metav1.NamespaceDefault,
			},
		}).
		Build()

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      first,
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      second,
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if got := prototype.GetName(); got != "" {
		t.Fatalf("expected prototype name to remain empty, got %q", got)
	}
}

func TestReconcileIgnoresMissingObject(t *testing.T) {
	t.Parallel()

	q := newMessageQueue(t)

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      "missing",
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePropagatesClientErrors(t *testing.T) {
	t.Parallel()

	const name = "resource"

	scheme := mustNewScheme(t)
	q := kubernetes.New(nil, scheme, &corev1.ConfigMap{})
	q.Client = &errorClient{err: errClientFailed}

	_, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	})
	if !errors.Is(err, errClientFailed) {
		t.Fatalf("expected %v, got %v", errClientFailed, err)
	}
}
