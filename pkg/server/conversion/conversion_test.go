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

package conversion_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/server/conversion"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	id           = "passport"
	name         = "cyril"
	description  = "some text"
	createdBy    = "shirley"
	modifiedBy   = "eric"
	tagKey       = "yale"
	tagValue     = "lock"
	organization = "acme"
	project      = "foo"
)

//nolint:gochecknoglobals
var (
	creationTime = time.Date(1970, 0, 0, 0, 0, 0, 0, time.UTC)
	deletionTime = time.Date(1980, 0, 0, 0, 0, 0, 0, time.UTC)
	modifiedTime = time.Date(1990, 0, 0, 0, 0, 0, 0, time.UTC)

	ErrAny = errors.New("some error happened")
)

type basicObject struct {
	metav1.ObjectMeta
}

func newBasicObject() *basicObject {
	return &basicObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:              id,
			CreationTimestamp: metav1.Time{Time: creationTime},
			Labels: map[string]string{
				constants.NameLabel: name,
			},
		},
	}
}

func (o *basicObject) StatusConditionRead(t unikornv1.ConditionType) (*unikornv1.Condition, error) {
	return nil, ErrAny
}

type advancedObject struct {
	metav1.ObjectMeta
}

func newAdvancedObject() *advancedObject {
	return &advancedObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:              id,
			CreationTimestamp: metav1.Time{Time: creationTime},
			DeletionTimestamp: &metav1.Time{Time: deletionTime},
			Labels: map[string]string{
				constants.NameLabel:         name,
				constants.OrganizationLabel: organization,
				constants.ProjectLabel:      project,
			},
			Annotations: map[string]string{
				constants.DescriptionAnnotation:       description,
				constants.CreatorAnnotation:           createdBy,
				constants.ModifierAnnotation:          modifiedBy,
				constants.ModifiedTimestampAnnotation: modifiedTime.Format(time.RFC3339),
			},
		},
	}
}

func (o *advancedObject) StatusConditionRead(t unikornv1.ConditionType) (*unikornv1.Condition, error) {
	return nil, ErrAny
}

func tags() unikornv1.TagList {
	return unikornv1.TagList{
		{
			Name:  tagKey,
			Value: tagValue,
		},
	}
}

// TestResourceReadMetadataBasic checks that a minimal input yields a minimal output.
func TestResourceReadMetadataBasic(t *testing.T) {
	t.Parallel()

	in := newBasicObject()

	out := conversion.ResourceReadMetadata(in, nil)

	require.Equal(t, id, out.Id)
	require.Equal(t, name, out.Name)
	require.Equal(t, creationTime, out.CreationTime)
	require.Equal(t, openapi.ResourceProvisioningStatusUnknown, out.ProvisioningStatus)
	require.Equal(t, openapi.ResourceHealthStatusUnknown, out.HealthStatus)

	require.Nil(t, out.Description)
	require.Nil(t, out.CreatedBy)
	require.Nil(t, out.ModifiedBy)
	require.Nil(t, out.ModifiedTime)
	require.Nil(t, out.DeletionTime)
	require.Nil(t, out.Tags)
}

// TestResourceReadMetadataAdvanced checks that a maximizes input yields a maximized output.
func TestResourceReadMetadataAdvanced(t *testing.T) {
	t.Parallel()

	in := newAdvancedObject()

	out := conversion.ResourceReadMetadata(in, tags())

	require.Equal(t, id, out.Id)
	require.Equal(t, name, out.Name)
	require.Equal(t, creationTime, out.CreationTime)
	require.Equal(t, openapi.ResourceProvisioningStatusDeprovisioning, out.ProvisioningStatus)
	require.Equal(t, openapi.ResourceHealthStatusUnknown, out.HealthStatus)

	require.Equal(t, ptr.To(description), out.Description)
	require.Equal(t, ptr.To(createdBy), out.CreatedBy)
	require.Equal(t, ptr.To(modifiedBy), out.ModifiedBy)
	require.Equal(t, ptr.To(modifiedTime), out.ModifiedTime)
	require.Equal(t, ptr.To(deletionTime), out.DeletionTime)
	require.NotNil(t, out.Tags)
	require.Len(t, *out.Tags, 1)
	require.Equal(t, tagKey, (*out.Tags)[0].Name)
	require.Equal(t, tagValue, (*out.Tags)[0].Value)
}

// TestOrganizationScopedResourceReadMetadataAdvanced tests that this extension of the advanced
// cases works woth all the extra data.
func TestOrganizationScopedResourceReadMetadataAdvanced(t *testing.T) {
	t.Parallel()

	in := newAdvancedObject()

	out := conversion.OrganizationScopedResourceReadMetadata(in, tags())

	require.Equal(t, id, out.Id)
	require.Equal(t, name, out.Name)
	require.Equal(t, creationTime, out.CreationTime)
	require.Equal(t, openapi.ResourceProvisioningStatusDeprovisioning, out.ProvisioningStatus)
	require.Equal(t, openapi.ResourceHealthStatusUnknown, out.HealthStatus)

	require.Equal(t, ptr.To(description), out.Description)
	require.Equal(t, ptr.To(createdBy), out.CreatedBy)
	require.Equal(t, ptr.To(modifiedBy), out.ModifiedBy)
	require.Equal(t, ptr.To(modifiedTime), out.ModifiedTime)
	require.Equal(t, ptr.To(deletionTime), out.DeletionTime)
	require.NotNil(t, out.Tags)
	require.Len(t, *out.Tags, 1)
	require.Equal(t, tagKey, (*out.Tags)[0].Name)
	require.Equal(t, tagValue, (*out.Tags)[0].Value)

	require.Equal(t, organization, out.OrganizationId)
}

// TestProjectScopedResourceReadMetadata tests that this extension of the advanced
// cases works woth all the extra data.
func TestProjectScopedResourceReadMetadata(t *testing.T) {
	t.Parallel()

	in := newAdvancedObject()

	out := conversion.ProjectScopedResourceReadMetadata(in, tags())

	require.Equal(t, id, out.Id)
	require.Equal(t, name, out.Name)
	require.Equal(t, creationTime, out.CreationTime)
	require.Equal(t, openapi.ResourceProvisioningStatusDeprovisioning, out.ProvisioningStatus)
	require.Equal(t, openapi.ResourceHealthStatusUnknown, out.HealthStatus)

	require.Equal(t, ptr.To(description), out.Description)
	require.Equal(t, ptr.To(createdBy), out.CreatedBy)
	require.Equal(t, ptr.To(modifiedBy), out.ModifiedBy)
	require.Equal(t, ptr.To(modifiedTime), out.ModifiedTime)
	require.Equal(t, ptr.To(deletionTime), out.DeletionTime)
	require.NotNil(t, out.Tags)
	require.Len(t, *out.Tags, 1)
	require.Equal(t, tagKey, (*out.Tags)[0].Name)
	require.Equal(t, tagValue, (*out.Tags)[0].Value)

	require.Equal(t, organization, out.OrganizationId)
	require.Equal(t, project, out.ProjectId)
}
