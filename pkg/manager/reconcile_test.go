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

//nolint:dupl
package manager_test

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"
	"github.com/unikorn-cloud/core/pkg/cd"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/manager"
	mockmanager "github.com/unikorn-cloud/core/pkg/manager/mock"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	mockprovisioners "github.com/unikorn-cloud/core/pkg/provisioners/mock"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	crmanager "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMain(m *testing.M) {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "Enables debug logging")
	flag.Parse()

	if debug {
		log.SetLogger(zap.New(zap.WriteTo(os.Stdout)))
	}

	m.Run()
}

// testContext provides a common framework for test execution.
type testContext struct {
	client client.Client
}

func mustNewTestContext(t *testing.T, objects ...client.Object) *testContext {
	t.Helper()

	scheme, err := coreclient.NewScheme()
	if err != nil {
		t.Fatal(err)
	}

	tc := &testContext{
		client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&unikornv1fake.ManagedResource{}).WithObjects(objects...).Build(),
	}

	return tc
}

func newNamespacedName(namespace, name string) types.NamespacedName {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	return namespacedName
}

//nolint:unparam
func newRequest(namespace, name string) reconcile.Request {
	request := reconcile.Request{
		NamespacedName: newNamespacedName(namespace, name),
	}

	return request
}

func (tc *testContext) newManager(c *gomock.Controller) crmanager.Manager {
	m := mockmanager.NewMockManager(c)

	m.EXPECT().GetClient().Return(tc.client).AnyTimes()

	return m
}

// mustAssertStatus checks the status is as we expect.  Very rudimentary as we only support
// the Available status.
func mustAssertStatus(t *testing.T, resource unikornv1.StatusConditionReader, status corev1.ConditionStatus, reason unikornv1.ConditionReason) {
	t.Helper()

	condition, err := resource.StatusConditionRead(unikornv1.ConditionAvailable)
	assert.NoError(t, err)

	if condition != nil {
		assert.Equal(t, status, condition.Status)
		assert.Equal(t, reason, condition.Reason)
	}
}

func managerOptions() *options.Options {
	return &options.Options{
		CDDriver: cd.DriverKindFlag{
			Kind: cd.DriverKindArgoCD,
		},
	}
}

const (
	testNamespace = "foo"
	testName      = "bar"
)

var (
	errUnhandled = errors.New("test error")
)

// TestReconcileDeleted tests that no error occurs when the resource is gone
// i.e. has been deleted.
func TestReconcileDeleted(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	tc := mustNewTestContext(t)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)
}

// TestReconcileCreate tests resource creation.
func TestReconcileCreate(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Provision(gomock.Any()).Return(nil)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource have all the correct metadata and status information set?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionTrue, unikornv1.ConditionReasonProvisioned)
}

// TestReconcileCreateYield tests resource creation and the status when the provisioner
// yields.
func TestReconcileCreateYield(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Provision(gomock.Any()).Return(provisioners.ErrYield)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource have all the correct metadata and status information set?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonProvisioning)
}

// TestReconcileCreateCancelled tests resource creation and the status when the context
// is cancelled.
func TestReconcileCreateCancelled(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
	}

	tc := mustNewTestContext(t, request)
	ctx, cancel := context.WithCancel(t.Context())

	cancel()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Provision(gomock.Any()).Return(ctx.Err())

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource have all the correct metadata and status information set?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonCancelled)
}

// TestReconcileCreateError tests resource creation and the status when an unhandled
// error is caught.
func TestReconcileCreateError(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Provision(gomock.Any()).Return(errUnhandled)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource have all the correct metadata and status information set?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
}

// TestReconcileDelete checks that a resource marked as being deleted has the
// finalizer removed and is cleaned up.
func TestReconcileDelete(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			Finalizers: []string{
				constants.Finalizer,
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Deprovision(gomock.Any()).Return(nil)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource still exist in Kubernetes?
	var result unikornv1fake.ManagedResource

	var apiError kerrors.APIStatus

	assert.ErrorAs(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result), &apiError)
	assert.Equal(t, metav1.StatusReasonNotFound, apiError.Status().Reason)
}

// TestReconcileDeleteYield checks that a resource marked as being deleted and
// yields due to a deprovision operation has the corrent status.
func TestReconcileDeleteYield(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			Finalizers: []string{
				constants.Finalizer,
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Deprovision(gomock.Any()).Return(provisioners.ErrYield)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource still exist in Kubernetes?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonDeprovisioning)
}

// TestReconcileDeleteCancelled checks that a resource marked as being deleted and
// whose context has been cancelled returns the correct status.
func TestReconcileDeleteCancelled(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			Finalizers: []string{
				constants.Finalizer,
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
		},
	}

	tc := mustNewTestContext(t, request)
	ctx, cancel := context.WithCancel(t.Context())

	cancel()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Deprovision(gomock.Any()).Return(ctx.Err())

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource still exist in Kubernetes?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonCancelled)
}

// TestReconcileDeleteError checks that a resource marked as being deleted and
// whose provisioner returns an unhandled error returns the correct status.
func TestReconcileDeleteError(t *testing.T) {
	t.Parallel()

	c := gomock.NewController(t)
	defer c.Finish()

	request := &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			Finalizers: []string{
				constants.Finalizer,
			},
			DeletionTimestamp: &metav1.Time{
				Time: time.Now(),
			},
		},
	}

	tc := mustNewTestContext(t, request)
	ctx := t.Context()

	p := mockprovisioners.NewMockManagerProvisioner(c)
	p.EXPECT().Object().Return(&unikornv1fake.ManagedResource{})
	p.EXPECT().Deprovision(gomock.Any()).Return(errUnhandled)

	reconciler := manager.NewReconciler(managerOptions(), nil, tc.newManager(c), func(_ manager.ControllerOptions) provisioners.ManagerProvisioner { return p })

	_, err := reconciler.Reconcile(ctx, newRequest(testNamespace, testName))
	assert.NoError(t, err)

	// Does the resource still exist in Kubernetes?
	var result unikornv1fake.ManagedResource

	assert.NoError(t, tc.client.Get(ctx, newNamespacedName(testNamespace, testName), &result))
	assert.Contains(t, result.Finalizers, constants.Finalizer)
	mustAssertStatus(t, &result, corev1.ConditionFalse, unikornv1.ConditionReasonErrored)
}
