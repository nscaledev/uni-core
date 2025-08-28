/*
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

package conversion

import (
	"encoding/json"
	"errors"
	"time"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var (
	ErrAnnotation = errors.New("a required annotation was missing")
)

// convertStatusCondition translates from Kubernetes status conditions to API ones.
func convertStatusCondition(in any) openapi.ResourceProvisioningStatus {
	// Not a resource with status conditions, consider it provisioned.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceProvisioningStatusProvisioned
	}

	// No condition yet, it's unknown.
	condition, err := reader.StatusConditionRead(unikornv1.ConditionAvailable)
	if err != nil {
		return openapi.ResourceProvisioningStatusUnknown
	}

	//nolint:exhaustive
	switch condition.Reason {
	case unikornv1.ConditionReasonProvisioning:
		return openapi.ResourceProvisioningStatusProvisioning
	case unikornv1.ConditionReasonProvisioned:
		return openapi.ResourceProvisioningStatusProvisioned
	case unikornv1.ConditionReasonErrored:
		return openapi.ResourceProvisioningStatusError
	case unikornv1.ConditionReasonDeprovisioning:
		return openapi.ResourceProvisioningStatusDeprovisioning
	}

	return openapi.ResourceProvisioningStatusUnknown
}

// convertHealthCondition translates from Kubernetes heath conditions to API ones.
func convertHealthCondition(in any) openapi.ResourceHealthStatus {
	// Not a resource with status conditions, consider it healthy.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceHealthStatusHealthy
	}

	// No condition yet, it's unknown.
	condition, err := reader.StatusConditionRead(unikornv1.ConditionHealthy)
	if err != nil {
		return openapi.ResourceHealthStatusUnknown
	}

	//nolint:exhaustive
	switch condition.Reason {
	case unikornv1.ConditionReasonHealthy:
		return openapi.ResourceHealthStatusHealthy
	case unikornv1.ConditionReasonDegraded:
		return openapi.ResourceHealthStatusDegraded
	case unikornv1.ConditionReasonErrored:
		return openapi.ResourceHealthStatusError
	}

	return openapi.ResourceHealthStatusUnknown
}

// ResourceReadMetadata extracts generic metadata from a resource for GET APIs.
func ResourceReadMetadata(in metav1.Object, tags unikornv1.TagList) openapi.ResourceReadMetadata {
	labels := in.GetLabels()
	annotations := in.GetAnnotations()

	out := openapi.ResourceReadMetadata{
		Id:                 in.GetName(),
		Name:               labels[constants.NameLabel],
		CreationTime:       in.GetCreationTimestamp().Time,
		ProvisioningStatus: convertStatusCondition(in),
		HealthStatus:       convertHealthCondition(in),
	}

	if v, ok := annotations[constants.DescriptionAnnotation]; ok {
		out.Description = &v
	}

	if v, ok := annotations[constants.CreatorAnnotation]; ok {
		out.CreatedBy = &v
	}

	if v, ok := annotations[constants.ModifierAnnotation]; ok {
		out.ModifiedBy = &v
	}

	if v, ok := annotations[constants.ModifiedTimestampAnnotation]; ok {
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			out.ModifiedTime = &t
		}
	}

	if v := in.GetDeletionTimestamp(); v != nil {
		out.DeletionTime = &v.Time
	}

	if len(tags) != 0 {
		out.Tags = ptr.To(ConvertTags(tags))
	}

	return out
}

// OrganizationScopedResourceReadMetadata extracts organization scoped metdata from a resource
// for GET APIS.
//
//nolint:errchkjson
func OrganizationScopedResourceReadMetadata(in metav1.Object, tags unikornv1.TagList) openapi.OrganizationScopedResourceReadMetadata {
	temp := ResourceReadMetadata(in, tags)

	tempJSON, _ := json.Marshal(temp)

	labels := in.GetLabels()

	out := openapi.OrganizationScopedResourceReadMetadata{
		OrganizationId: labels[constants.OrganizationLabel],
	}

	_ = json.Unmarshal(tempJSON, &out)

	return out
}

// ProjectScopedResourceReadMetadata extracts project scoped metdata from a resource for
// GET APIs.
//
//nolint:errchkjson
func ProjectScopedResourceReadMetadata(in metav1.Object, tags unikornv1.TagList) openapi.ProjectScopedResourceReadMetadata {
	temp := ResourceReadMetadata(in, tags)

	tempJSON, _ := json.Marshal(temp)

	labels := in.GetLabels()

	out := openapi.ProjectScopedResourceReadMetadata{
		OrganizationId: labels[constants.OrganizationLabel],
		ProjectId:      labels[constants.ProjectLabel],
	}

	_ = json.Unmarshal(tempJSON, &out)

	return out
}

// ObjectMetadata implements a builder pattern.
type ObjectMetadata metav1.ObjectMeta

// NewObjectMetadata requests the bare minimum to build an object metadata object.
func NewObjectMetadata(metadata *openapi.ResourceWriteMetadata, namespace string) *ObjectMetadata {
	o := &ObjectMetadata{
		Namespace: namespace,
		Name:      util.GenerateResourceID(),
		Labels: map[string]string{
			constants.NameLabel: metadata.Name,
		},
		Annotations: map[string]string{},
	}

	if metadata.Description != nil {
		o.Annotations[constants.DescriptionAnnotation] = *metadata.Description
	}

	return o
}

// WithOrganization adds an organization for scoped resources.
func (o *ObjectMetadata) WithOrganization(id string) *ObjectMetadata {
	o.Labels[constants.OrganizationLabel] = id

	return o
}

// WithProject adds a project for scoped resources.
func (o *ObjectMetadata) WithProject(id string) *ObjectMetadata {
	o.Labels[constants.ProjectLabel] = id

	return o
}

// WithLabel allows non-generic labels to be attached to a resource.
func (o *ObjectMetadata) WithLabel(key, value string) *ObjectMetadata {
	o.Labels[key] = value

	return o
}

// Get renders the object metadata ready for inclusion into a Kubernetes resource.
func (o *ObjectMetadata) Get() metav1.ObjectMeta {
	return metav1.ObjectMeta(*o)
}

// MetadataMutationFunc is used to mutate metadata on update.
type MetadataMutationFunc func(required, current metav1.Object) error

// UpdateObjectMetadata abstracts away metadata updates.
func UpdateObjectMetadata(required, current metav1.Object, mutators ...MetadataMutationFunc) error {
	req := required.GetAnnotations()
	if req == nil {
		req = map[string]string{}
	}

	req[constants.ModifiedTimestampAnnotation] = time.Now().UTC().Format(time.RFC3339)

	required.SetAnnotations(req)

	for _, m := range mutators {
		if err := m(required, current); err != nil {
			return err
		}
	}

	return nil
}

func ConvertTag(in unikornv1.Tag) openapi.Tag {
	out := openapi.Tag{
		Name:  in.Name,
		Value: in.Value,
	}

	return out
}

func ConvertTags(in unikornv1.TagList) openapi.TagList {
	if in == nil {
		return nil
	}

	out := make(openapi.TagList, len(in))

	for i := range in {
		out[i] = ConvertTag(in[i])
	}

	return out
}

func GenerateTag(in openapi.Tag) unikornv1.Tag {
	out := unikornv1.Tag{
		Name:  in.Name,
		Value: in.Value,
	}

	return out
}

func GenerateTagList(in *openapi.TagList) unikornv1.TagList {
	if in == nil {
		return nil
	}

	out := make(unikornv1.TagList, len(*in))

	for i := range *in {
		out[i] = GenerateTag((*in)[i])
	}

	return out
}
