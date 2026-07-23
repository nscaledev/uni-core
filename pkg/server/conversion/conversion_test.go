/*
Copyright 2025 the Unikorn Authors.
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

package conversion_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
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

func (o *basicObject) StatusConditionRead(t unikornv1.ConditionType) (*metav1.Condition, error) {
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

func (o *advancedObject) StatusConditionRead(t unikornv1.ConditionType) (*metav1.Condition, error) {
	return nil, ErrAny
}

// legacyReasonObject carries an Available condition whose reason is outside the
// provisioning vocabulary, e.g. one written by an older core (such as the
// retired Cancelled reason).
type legacyReasonObject struct {
	metav1.ObjectMeta
}

func newLegacyReasonObject() *legacyReasonObject {
	return &legacyReasonObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:              id,
			CreationTimestamp: metav1.Time{Time: creationTime},
			Labels: map[string]string{
				constants.NameLabel: name,
			},
		},
	}
}

func (o *legacyReasonObject) StatusConditionRead(t unikornv1.ConditionType) (*metav1.Condition, error) {
	return &metav1.Condition{
		Type:   string(unikornv1.ConditionAvailable),
		Status: metav1.ConditionFalse,
		Reason: "Cancelled",
	}, nil
}

func tags() unikornv1.TagList {
	return unikornv1.TagList{
		{
			Name:  tagKey,
			Value: tagValue,
		},
	}
}

// TestNewDeterministicObjectMetadata checks the deterministic constructor sets the
// expected Kubernetes metadata fields and that its output is stable.
func TestNewDeterministicObjectMetadata(t *testing.T) {
	t.Parallel()

	ns := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

	meta := &openapi.ResourceWriteMetadata{Name: name, Description: ptr.To(description)}

	a := conversion.NewDeterministicObjectMetadata(meta, "default", ns, "net-1:host-1").Get()
	b := conversion.NewDeterministicObjectMetadata(meta, "default", ns, "net-1:host-1").Get()

	require.Equal(t, a.Name, b.Name)
	require.Equal(t, "default", a.Namespace)
	require.Equal(t, name, a.Labels[constants.NameLabel])
	require.Equal(t, description, a.Annotations[constants.DescriptionAnnotation])

	// Different invariant must yield a different name.
	c := conversion.NewDeterministicObjectMetadata(meta, "default", ns, "net-1:host-2").Get()
	require.NotEqual(t, a.Name, c.Name)
}

// TestResourceReadMetadataBasic checks that a minimal input yields a minimal output.
func TestResourceReadMetadataBasic(t *testing.T) {
	t.Parallel()

	in := newBasicObject()

	out := conversion.ResourceReadMetadata(in, nil)

	require.Equal(t, id, out.Id)
	require.Equal(t, name, out.Name)
	require.Equal(t, creationTime, out.CreationTime)
	require.Equal(t, openapi.ResourceProvisioningStatusPending, out.ProvisioningStatus)
	require.Equal(t, openapi.ResourceHealthStatusUnknown, out.HealthStatus)

	require.Nil(t, out.Description)
	require.Nil(t, out.CreatedBy)
	require.Nil(t, out.ModifiedBy)
	require.Nil(t, out.ModifiedTime)
	require.Nil(t, out.DeletionTime)
	require.Nil(t, out.Tags)
}

// TestResourceReadMetadataUnknownReason checks that an Available reason outside the
// provisioning vocabulary falls back to provisioning, never an invalid empty status.
func TestResourceReadMetadataUnknownReason(t *testing.T) {
	t.Parallel()

	in := newLegacyReasonObject()

	out := conversion.ResourceReadMetadata(in, nil)

	require.Equal(t, openapi.ResourceProvisioningStatusProvisioning, out.ProvisioningStatus)
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
