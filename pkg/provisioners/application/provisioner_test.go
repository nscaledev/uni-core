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

package application_test

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	unikornv1fake "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1/fake"
	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/cd/mock"
	coreclient "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	"github.com/unikorn-cloud/core/pkg/provisioners/application"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newManagedResource() *unikornv1fake.ManagedResource {
	return &unikornv1fake.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Name: "bar",
			Labels: map[string]string{
				"2": "foo",
				"3": "bar",
				"1": "baz",
			},
		},
	}
}

func newManagedResourceLabels() []cd.ResourceIdentifierLabel {
	return []cd.ResourceIdentifierLabel{
		{
			Name:  "1",
			Value: "baz",
		},
		{
			Name:  "2",
			Value: "foo",
		},
		{
			Name:  "3",
			Value: "bar",
		},
	}
}

// testContext provides a common framework for test execution.
type testContext struct {
	client client.Client
}

func mustNewTestContext(t *testing.T) *testContext {
	t.Helper()

	scheme, err := coreclient.NewScheme()
	if err != nil {
		t.Fatal(err)
	}

	tc := &testContext{
		client: fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&unikornv1fake.ManagedResource{}).Build(),
	}

	return tc
}

const (
	baseNamespace   = "scooby-doo"
	applicationID   = "c785837a-7412-49a6-ac7e-6d75ab6ca577"
	applicationName = "test"
	repo            = "foo"
	chart           = "bar"
)

var (
	version = unikornv1.SemanticVersion{
		Version: *semver.MustParse("1.2.3"),
	}
)

func applicationGetter(application *unikornv1.HelmApplication) application.GetterFunc {
	return func(_ context.Context) (*unikornv1.HelmApplication, *unikornv1.SemanticVersion, error) {
		return application, &version, nil
	}
}

// TestApplicationCreateHelm tests that given the requested input the provisioner
// creates a CD Application, and the fields are populated as expected.
func TestApplicationCreateHelm(t *testing.T) {
	t.Parallel()

	app := &unikornv1.HelmApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseNamespace,
			Name:      applicationID,
			Labels: map[string]string{
				constants.NameLabel: applicationName,
			},
		},
		Spec: unikornv1.HelmApplicationSpec{
			Versions: []unikornv1.HelmApplicationVersion{
				{

					Repo:    ptr.To(repo),
					Chart:   ptr.To(chart),
					Version: version,
				},
			},
		},
	}

	tc := mustNewTestContext(t)

	c := gomock.NewController(t)
	defer c.Finish()

	driverAppID := &cd.ResourceIdentifier{
		Name:   applicationName,
		Labels: newManagedResourceLabels(),
	}

	driverApp := &cd.HelmApplication{
		Repo:      repo,
		Chart:     chart,
		Version:   version.Original(),
		Namespace: "default",
	}

	driver := mock.NewMockDriver(c)
	owner := newManagedResource()

	clusterContext := &coreclient.ClusterContext{
		Client: tc.client,
	}

	ctx := t.Context()
	ctx = coreclient.NewContextWithNamespace(ctx, baseNamespace)
	ctx = coreclient.NewContext(ctx, tc.client)
	ctx = coreclient.NewContextWithCluster(ctx, clusterContext)
	ctx = cd.NewContext(ctx, driver)
	ctx = application.NewContext(ctx, owner)

	driver.EXPECT().CreateOrUpdateHelmApplication(ctx, driverAppID, driverApp).Return(provisioners.ErrYield)

	provisioner := application.New(applicationGetter(app))

	assert.ErrorIs(t, provisioner.Provision(ctx), provisioners.ErrYield)
}

// TestApplicationCreateHelmExtended tests that given the requested input the provisioner
// creates an ArgoCD Application, and the fields are populated as expected.
func TestApplicationCreateHelmExtended(t *testing.T) {
	t.Parallel()

	release := "epic"
	parameter := "foo"
	value := "bah"
	remoteClusterName := "bar"
	remoteClusterLabel1 := "dog"
	remoteClusterLabel1Value := "woof"
	remoteClusterLabel2 := "cat"
	remoteClusterLabel2value := "meow"

	app := &unikornv1.HelmApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseNamespace,
			Name:      applicationID,
			Labels: map[string]string{
				constants.NameLabel: applicationName,
			},
		},
		Spec: unikornv1.HelmApplicationSpec{
			Versions: []unikornv1.HelmApplicationVersion{
				{
					Repo:    ptr.To(repo),
					Chart:   ptr.To(chart),
					Version: version,
					Release: ptr.To(release),
					Parameters: []unikornv1.HelmApplicationParameter{
						{
							Name:  parameter,
							Value: value,
						},
					},
					CreateNamespace: ptr.To(true),
					ServerSideApply: ptr.To(true),
				},
			},
		},
	}

	tc := mustNewTestContext(t)

	c := gomock.NewController(t)
	defer c.Finish()

	remoteID := &cd.ResourceIdentifier{
		Name: remoteClusterName,
		Labels: []cd.ResourceIdentifierLabel{
			{
				Name:  remoteClusterLabel1,
				Value: remoteClusterLabel1Value,
			},
			{
				Name:  remoteClusterLabel2,
				Value: remoteClusterLabel2value,
			},
		},
	}

	driverAppID := &cd.ResourceIdentifier{
		Name:   applicationName,
		Labels: newManagedResourceLabels(),
	}

	driverApp := &cd.HelmApplication{
		Repo:    repo,
		Chart:   chart,
		Version: version.Original(),
		Release: release,
		Parameters: []cd.HelmApplicationParameter{
			{
				Name:  parameter,
				Value: value,
			},
		},
		Cluster:         remoteID,
		Namespace:       "default",
		CreateNamespace: true,
		ServerSideApply: true,
		AllowDegraded:   true,
	}

	driver := mock.NewMockDriver(c)
	owner := newManagedResource()

	clusterContext := &coreclient.ClusterContext{
		ID:     remoteID,
		Client: tc.client,
	}

	ctx := t.Context()
	ctx = coreclient.NewContextWithNamespace(ctx, baseNamespace)
	ctx = coreclient.NewContext(ctx, tc.client)
	ctx = coreclient.NewContextWithCluster(ctx, clusterContext)
	ctx = cd.NewContext(ctx, driver)
	ctx = application.NewContext(ctx, owner)

	driver.EXPECT().CreateOrUpdateHelmApplication(ctx, driverAppID, driverApp).Return(provisioners.ErrYield)

	provisioner := application.New(applicationGetter(app)).AllowDegraded()

	assert.ErrorIs(t, provisioner.Provision(ctx), provisioners.ErrYield)
}

// TestApplicationCreateGit tests that given the requested input the provisioner
// creates an ArgoCD Application, and the fields are populated as expected.
func TestApplicationCreateGit(t *testing.T) {
	t.Parallel()

	path := "bar"
	branch := "groot"

	app := &unikornv1.HelmApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseNamespace,
			Name:      applicationID,
			Labels: map[string]string{
				constants.NameLabel: applicationName,
			},
		},
		Spec: unikornv1.HelmApplicationSpec{
			Versions: []unikornv1.HelmApplicationVersion{
				{

					Repo:    ptr.To(repo),
					Path:    ptr.To(path),
					Version: version,
					Branch:  ptr.To(branch),
				},
			},
		},
	}

	tc := mustNewTestContext(t)

	c := gomock.NewController(t)
	defer c.Finish()

	driverAppID := &cd.ResourceIdentifier{
		Name:   applicationName,
		Labels: newManagedResourceLabels(),
	}

	driverApp := &cd.HelmApplication{
		Repo:      repo,
		Path:      path,
		Version:   version.Original(),
		Branch:    branch,
		Namespace: "default",
	}

	driver := mock.NewMockDriver(c)
	owner := newManagedResource()

	clusterContext := &coreclient.ClusterContext{
		Client: tc.client,
	}

	ctx := t.Context()
	ctx = coreclient.NewContextWithNamespace(ctx, baseNamespace)
	ctx = coreclient.NewContext(ctx, tc.client)
	ctx = coreclient.NewContextWithCluster(ctx, clusterContext)
	ctx = cd.NewContext(ctx, driver)
	ctx = application.NewContext(ctx, owner)

	driver.EXPECT().CreateOrUpdateHelmApplication(ctx, driverAppID, driverApp).Return(provisioners.ErrYield)

	provisioner := application.New(applicationGetter(app))

	assert.ErrorIs(t, provisioner.Provision(ctx), provisioners.ErrYield)
}

const (
	mutatorRelease                  = "sentinel"
	mutatorParameter                = "foo"
	mutatorValue                    = "bar"
	mutatorIgnoreDifferencesGroup   = "hippes"
	mutatorIgnoreDifferencesKind    = "treeHugger"
	mutatorIgnoreDifferencesPointer = "arrow"
)

//nolint:gochecknoglobals
var mutatorValues = map[string]string{
	mutatorParameter: mutatorValue,
}

//nolint:gochecknoglobals
var mutatorLabels = map[string]string{
	"origin":       "cluster-1",
	"organisation": "uni",
}

//nolint:gochecknoglobals
var mutatorAnnotations = map[string]string{
	"colour": "rainbow",
	"wings":  "no",
}

// mutator does just that allows modifications of the application.
type mutator struct {
	postProvisionCalled bool
}

var _ application.ReleaseNamer = &mutator{}
var _ application.Paramterizer = &mutator{}
var _ application.ValuesGenerator = &mutator{}
var _ application.NamespaceLabeler = &mutator{}
var _ application.Customizer = &mutator{}
var _ application.PostProvisionHook = &mutator{}

func (m *mutator) ReleaseName(ctx context.Context) string {
	return "sentinel"
}

func (m *mutator) Parameters(ctx context.Context, version unikornv1.SemanticVersion) (map[string]string, error) {
	p := map[string]string{
		mutatorParameter: mutatorValue,
	}

	return p, nil
}

func (m *mutator) Values(ctx context.Context, version unikornv1.SemanticVersion) (any, error) {
	return mutatorValues, nil
}

func (m *mutator) NamespaceMetadata(_ context.Context, _ unikornv1.SemanticVersion) (map[string]string, map[string]string, error) {
	return mutatorLabels, mutatorAnnotations, nil
}

func (m *mutator) Customize(version unikornv1.SemanticVersion) ([]cd.HelmApplicationField, error) {
	differences := []cd.HelmApplicationField{
		{
			Group: mutatorIgnoreDifferencesGroup,
			Kind:  mutatorIgnoreDifferencesKind,
			JSONPointers: []string{
				mutatorIgnoreDifferencesPointer,
			},
		},
	}

	return differences, nil
}

func (m *mutator) PostProvision(_ context.Context) error {
	m.postProvisionCalled = true

	return nil
}

// TestApplicationCreateMutate tests that given the requested input the provisioner
// creates an ArgoCD Application, and the fields are populated as expected.
func TestApplicationCreateMutate(t *testing.T) {
	t.Parallel()

	namespace := "gerbils"

	app := &unikornv1.HelmApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseNamespace,
			Name:      applicationID,
			Labels: map[string]string{
				constants.NameLabel: applicationName,
			},
		},
		Spec: unikornv1.HelmApplicationSpec{
			Versions: []unikornv1.HelmApplicationVersion{
				{

					Repo:    ptr.To(repo),
					Chart:   ptr.To(chart),
					Version: version,
				},
			},
		},
	}

	tc := mustNewTestContext(t)

	c := gomock.NewController(t)
	defer c.Finish()

	driverAppID := &cd.ResourceIdentifier{
		Name:   applicationName,
		Labels: newManagedResourceLabels(),
	}

	driverApp := &cd.HelmApplication{
		Repo:      repo,
		Chart:     chart,
		Version:   version.Original(),
		Release:   mutatorRelease,
		Namespace: namespace,
		Parameters: []cd.HelmApplicationParameter{
			{
				Name:  mutatorParameter,
				Value: mutatorValue,
			},
		},
		Values: mutatorValues,
		IgnoreDifferences: []cd.HelmApplicationField{
			{
				Group: mutatorIgnoreDifferencesGroup,
				Kind:  mutatorIgnoreDifferencesKind,
				JSONPointers: []string{
					mutatorIgnoreDifferencesPointer,
				},
			},
		},
	}
	driverApp.NamespaceMetadata.Labels = mutatorLabels
	driverApp.NamespaceMetadata.Annotations = mutatorAnnotations

	driver := mock.NewMockDriver(c)
	owner := newManagedResource()

	clusterContext := &coreclient.ClusterContext{
		Client: tc.client,
	}

	ctx := t.Context()
	ctx = coreclient.NewContextWithNamespace(ctx, baseNamespace)
	ctx = coreclient.NewContext(ctx, tc.client)
	ctx = coreclient.NewContextWithCluster(ctx, clusterContext)
	ctx = cd.NewContext(ctx, driver)
	ctx = application.NewContext(ctx, owner)

	driver.EXPECT().CreateOrUpdateHelmApplication(ctx, driverAppID, driverApp).Return(nil)

	mutator := &mutator{}

	provisioner := application.New(applicationGetter(app)).WithGenerator(mutator).InNamespace(namespace)

	assert.NoError(t, provisioner.Provision(ctx))
	assert.True(t, mutator.postProvisionCalled)
}

// TestApplicationDeleteNotFound tests the provisioner returns nil when an application
// doesn't exist.
func TestApplicationDeleteNotFound(t *testing.T) {
	t.Parallel()

	app := &unikornv1.HelmApplication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: baseNamespace,
			Name:      applicationID,
			Labels: map[string]string{
				constants.NameLabel: applicationName,
			},
		},
		Spec: unikornv1.HelmApplicationSpec{
			Versions: []unikornv1.HelmApplicationVersion{
				{

					Repo:    ptr.To(repo),
					Chart:   ptr.To(chart),
					Version: version,
				},
			},
		},
	}

	tc := mustNewTestContext(t)

	c := gomock.NewController(t)
	defer c.Finish()

	driverAppID := &cd.ResourceIdentifier{
		Name:   applicationName,
		Labels: newManagedResourceLabels(),
	}

	driver := mock.NewMockDriver(c)
	owner := newManagedResource()

	ctx := t.Context()
	ctx = coreclient.NewContextWithNamespace(ctx, baseNamespace)
	ctx = coreclient.NewContext(ctx, tc.client)
	ctx = cd.NewContext(ctx, driver)
	ctx = application.NewContext(ctx, owner)

	driver.EXPECT().DeleteHelmApplication(ctx, driverAppID, false).Return(provisioners.ErrYield)

	provisioner := application.New(applicationGetter(app))

	assert.ErrorIs(t, provisioner.Deprovision(ctx), provisioners.ErrYield)
}
