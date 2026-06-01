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
	"time"

	"github.com/go-logr/logr"
	"go.uber.org/mock/gomock"

	mockmanager "github.com/unikorn-cloud/core/pkg/manager/mock"
	"github.com/unikorn-cloud/core/pkg/messaging"
	"github.com/unikorn-cloud/core/pkg/messaging/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	cr "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
)

var (
	errClientFailed   = errors.New("client failed")
	errConsumerFailed = errors.New("consumer failed")
)

type errorClient struct {
	client.Client
	err error
}

func (c *errorClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

type recordingConsumer struct {
	envelopes []*messaging.Envelope
	err       error
}

func (c *recordingConsumer) Consume(_ context.Context, envelope *messaging.Envelope) error {
	c.envelopes = append(c.envelopes, envelope)

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

func setupQueueWithManager(t *testing.T, consumer messaging.Consumer, objects ...client.Object) *kubernetes.MessageQueue {
	t.Helper()

	scheme := mustNewScheme(t)
	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
	skipNameValidation := true

	manager := mockmanager.NewMockManager(gomock.NewController(t))
	manager.EXPECT().GetClient().Return(cli)
	manager.EXPECT().GetControllerOptions().Return(ctrlconfig.Controller{
		SkipNameValidation: &skipNameValidation,
	}).AnyTimes()
	manager.EXPECT().GetScheme().Return(scheme).AnyTimes()
	manager.EXPECT().GetLogger().Return(logr.Discard()).AnyTimes()
	manager.EXPECT().Add(gomock.Any()).Return(nil)
	manager.EXPECT().GetCache().Return(nil)

	q := kubernetes.NewForManager(&corev1.ConfigMap{})
	if err := q.SetupWithManager(manager, consumer); err != nil {
		t.Fatal(err)
	}

	return q
}

func TestSetupWithManagerDeliversEnvelopeFromFetchedObject(t *testing.T) {
	t.Parallel()

	const name = "resource"

	consumer := &recordingConsumer{}
	q := setupQueueWithManager(t, consumer, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	})

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if len(consumer.envelopes) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(consumer.envelopes))
	}

	if got := consumer.envelopes[0].ResourceID; got != name {
		t.Fatalf("expected resource ID %q, got %q", name, got)
	}
}

func TestSetupWithManagerDeliversDeletionTimestampFromFetchedObject(t *testing.T) {
	t.Parallel()

	const name = "resource"

	deletionTimestamp := metav1.NewTime(time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC))
	consumer := &recordingConsumer{}
	q := setupQueueWithManager(t, consumer, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         metav1.NamespaceDefault,
			Finalizers:        []string{"test"},
			DeletionTimestamp: &deletionTimestamp,
		},
	})

	if _, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	}); err != nil {
		t.Fatal(err)
	}

	if len(consumer.envelopes) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(consumer.envelopes))
	}

	got := consumer.envelopes[0].DeletionTimestamp
	if got == nil {
		t.Fatal("expected deletion timestamp")
	}

	if !got.Equal(deletionTimestamp.Time) {
		t.Fatalf("expected deletion timestamp %s, got %s", deletionTimestamp.Time, *got)
	}
}

func TestReconcileReturnsConsumerError(t *testing.T) {
	t.Parallel()

	const name = "resource"

	consumer := &recordingConsumer{err: errConsumerFailed}
	q := setupQueueWithManager(t, consumer, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	})

	_, err := q.Reconcile(t.Context(), cr.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	})
	if !errors.Is(err, errConsumerFailed) {
		t.Fatalf("expected %v, got %v", errConsumerFailed, err)
	}
}

func TestReconcileDoesNotMutatePrototype(t *testing.T) {
	t.Parallel()

	const name = "resource"

	scheme := mustNewScheme(t)
	prototype := &corev1.ConfigMap{}
	q := kubernetes.NewForManager(prototype)
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
	q := kubernetes.NewForManager(prototype)
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
