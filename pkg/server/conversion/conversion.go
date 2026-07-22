/*
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

package conversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/google/uuid"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/openapi"
	"github.com/unikorn-cloud/core/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrAnnotation = errors.New("a required annotation was missing")
)

// convertStatusCondition translates from Kubernetes status conditions to API ones.
func convertStatusCondition(in metav1.Object) openapi.ResourceProvisioningStatus {
	// We set the status after a reconcile, so this allows us to
	// reflect the correct state to the user immediately.
	if in.GetDeletionTimestamp() != nil {
		return openapi.ResourceProvisioningStatusDeprovisioning
	}

	// Not a resource with status conditions, consider it provisioned.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceProvisioningStatusProvisioned
	}

	// No condition yet, it's pending.
	condition, err := unikornv1.GetAvailableCondition(reader)
	if err != nil {
		return openapi.ResourceProvisioningStatusPending
	}

	// The reason vocabulary is open (metav1.Condition uses a pattern, not an
	// enum), so we classify each known reason by its coarse disposition. The
	// Dependency* failure reasons split by whether they can self-heal: NotReady
	// and Failed are yields that project to provisioning (still in flight),
	// NotFound is terminal and projects to error. Anything we do not recognise —
	// a legacy value from an older core, or a newer reason a producer added that
	// this reader predates — falls through to the optimistic provisioning default
	// (the resource exists and is presumed in flight; a genuinely absent condition
	// is handled above as pending, so we never surface an empty, non-enum status),
	// and we warn so an operator can spot a reason the projection has not caught up
	// with rather than a silent permanent spinner.
	switch condition.Reason {
	case unikornv1.ConditionReasonProvisioning:
		return openapi.ResourceProvisioningStatusProvisioning
	case unikornv1.ConditionReasonProvisioned:
		return openapi.ResourceProvisioningStatusProvisioned
	case unikornv1.ConditionReasonErrored, unikornv1.ConditionReasonDependencyNotFound:
		return openapi.ResourceProvisioningStatusError
	case unikornv1.ConditionReasonDeprovisioned, unikornv1.ConditionReasonDeprovisioning:
		return openapi.ResourceProvisioningStatusDeprovisioning
	case unikornv1.ConditionReasonDependencyNotReady, unikornv1.ConditionReasonDependencyFailed:
		return openapi.ResourceProvisioningStatusProvisioning
	}

	log.Log.Info("unrecognised provisioning condition reason; defaulting to provisioning status",
		"reason", condition.Reason, "resource", in.GetName())

	return openapi.ResourceProvisioningStatusProvisioning
}

// convertHealthCondition translates from Kubernetes heath conditions to API ones.
func convertHealthCondition(in metav1.Object) openapi.ResourceHealthStatus {
	// Not a resource with status conditions, consider it healthy.
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return openapi.ResourceHealthStatusHealthy
	}

	// No condition yet, it's unknown.
	condition, err := unikornv1.GetHealthyCondition(reader)
	if err != nil {
		return openapi.ResourceHealthStatusUnknown
	}

	var out openapi.ResourceHealthStatus

	switch condition.Reason {
	case unikornv1.ConditionReasonHealthy:
		out = openapi.ResourceHealthStatusHealthy
	case unikornv1.ConditionReasonDegraded:
		out = openapi.ResourceHealthStatusDegraded
	case unikornv1.ConditionReasonUnknown:
		out = openapi.ResourceHealthStatusUnknown
	}

	return out
}

// convertProvisioningStatusDetail projects the resource's Available condition into
// the API provisioning detail: the closed-vocabulary reason and the user-safe
// message. It supplements the coarse provisioningStatus with the "why", is derived
// read-time (never stored), and returns nil — so the field is omitted — when the
// resource carries no Available condition yet.
func convertProvisioningStatusDetail(in metav1.Object) *openapi.ProvisioningStatusDetail {
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return nil
	}

	condition, err := unikornv1.GetAvailableCondition(reader)
	if err != nil {
		return nil
	}

	return &openapi.ProvisioningStatusDetail{
		Reason:  openapi.ProvisioningStatusReason(condition.Reason),
		Message: condition.Message,
	}
}

// convertHealthStatusDetail projects the resource's Healthy condition into the API
// health detail: the closed-vocabulary reason and the user-safe message (the
// "why", e.g. "2/12 nodes are down"). It supplements the coarse healthStatus, is
// derived read-time (never stored), and returns nil — so the field is omitted —
// when the resource carries no Healthy condition yet.
func convertHealthStatusDetail(in metav1.Object) *openapi.HealthStatusDetail {
	reader, ok := in.(unikornv1.StatusConditionReader)
	if !ok {
		return nil
	}

	condition, err := unikornv1.GetHealthyCondition(reader)
	if err != nil {
		return nil
	}

	return &openapi.HealthStatusDetail{
		Reason:  openapi.HealthStatusReason(condition.Reason),
		Message: condition.Message,
	}
}

// ResourceReadMetadata extracts generic metadata from a resource for GET APIs.
func ResourceReadMetadata(in metav1.Object, tags unikornv1.TagList) openapi.ResourceReadMetadata {
	labels := in.GetLabels()
	annotations := in.GetAnnotations()

	out := openapi.ResourceReadMetadata{
		Id:                       in.GetName(),
		Name:                     labels[constants.NameLabel],
		CreationTime:             in.GetCreationTimestamp().Time,
		ProvisioningStatus:       convertStatusCondition(in),
		ProvisioningStatusDetail: convertProvisioningStatusDetail(in),
		HealthStatus:             convertHealthCondition(in),
		HealthStatusDetail:       convertHealthStatusDetail(in),
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

// NewDeterministicObjectMetadata is like NewObjectMetadata but derives the Kubernetes
// resource name deterministically from idNamespace and invariant using UUID v5. This
// enables Kubernetes 409 conflict detection for duplicate logical resources — a second
// create with the same invariant will always collide with the first.
// Each resource type should define its own idNamespace constant to prevent cross-type
// collisions. invariant must be derived from stable, immutable fields; time-varying
// or mutable values silently break the deduplication guarantee.
func NewDeterministicObjectMetadata(metadata *openapi.ResourceWriteMetadata, namespace string, idNamespace uuid.UUID, invariant string) *ObjectMetadata {
	o := &ObjectMetadata{
		Namespace: namespace,
		Name:      util.GenerateDeterministicResourceID(idNamespace, invariant),
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

// LogUpdate takes a diff of two resources and logs them.
func LogUpdate(ctx context.Context, current, required metav1.Object) error {
	log := log.FromContext(ctx)

	currentJSON, err := json.Marshal(current)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal current object", err)
	}

	requiredJSON, err := json.Marshal(required)
	if err != nil {
		return fmt.Errorf("%w: failed to marshal required object", err)
	}

	patch, err := jsonpatch.CreateMergePatch(currentJSON, requiredJSON)
	if err != nil {
		return fmt.Errorf("%w: failed to generate merge patch", err)
	}

	var patchRaw any

	if err := json.Unmarshal(patch, &patchRaw); err != nil {
		return fmt.Errorf("%w: failed to unmarshal merge patch", err)
	}

	var currentRaw any

	if err := json.Unmarshal(currentJSON, &currentRaw); err != nil {
		return fmt.Errorf("%w: failed to current raw", err)
	}

	log.Info("patching resource", "current", currentRaw, "patch", patchRaw)

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
