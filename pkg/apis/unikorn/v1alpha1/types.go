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

package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/Masterminds/semver/v3"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/structured-merge-diff/v4/value"
)

var (
	ErrJSONUnmarshal = errors.New("failed to unmarshal JSON")
)

// SemanticVersion allows semver in either v1.0.0 or 1.0.0 forms, although the latter is
// technically the only correct one, things like Helm allow the former.
// +kubebuilder:validation:Type=string
// +kubebuilder:validation:Pattern="^v?[0-9]+(\\.[0-9]+)?(\\.[0-9]+)?(-([0-9A-Za-z\\-]+(\\.[0-9A-Za-z\\-]+)*))?(\\+([0-9A-Za-z\\-]+(\\.[0-9A-Za-z\\-]+)*))?$"
type SemanticVersion struct {
	semver.Version
}

func (v *SemanticVersion) Compare(o *SemanticVersion) int {
	return v.Version.Compare(&o.Version)
}

func (v *SemanticVersion) Equal(o *SemanticVersion) bool {
	return v.Version.Equal(&o.Version)
}

func (v *SemanticVersion) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &v.Version)
}

func (v *SemanticVersion) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Original())
}

func (v *SemanticVersion) ToUnstructured() any {
	return v.Original()
}

// SemanticVersionConstraints allows an arbitrary semantic version to be constrained.
// +kubebuilder:validation:Type=string
// +kubebuilder:object:generate=false
type SemanticVersionConstraints struct {
	semver.Constraints
}

func (c *SemanticVersionConstraints) Check(v *SemanticVersion) bool {
	return c.Constraints.Check(&v.Version)
}

func (c *SemanticVersionConstraints) String() string {
	return c.Constraints.String()
}

func (c *SemanticVersionConstraints) UnmarshalJSON(b []byte) error {
	var s string

	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	constraints, err := semver.NewConstraint(s)
	if err != nil {
		return err
	}

	c.Constraints = *constraints

	return nil
}

func (c *SemanticVersionConstraints) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Constraints.String())
}

func (c *SemanticVersionConstraints) ToUnstructured() any {
	return c.Constraints.String()
}

func (c *SemanticVersionConstraints) DeepCopyInto(out *SemanticVersionConstraints) {
	t, _ := c.MarshalText()
	_ = out.UnmarshalText(t)
}

func (c *SemanticVersionConstraints) DeepCopy() *SemanticVersionConstraints {
	out := &SemanticVersionConstraints{}
	c.DeepCopyInto(out)

	return out
}

// +kubebuilder:validation:Type=string
// +kubebuilder:validation:Pattern="^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])\\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])$"
type IPv4Address struct {
	net.IP
}

// Ensure the type implements json.Unmarshaler.
var _ = json.Unmarshaler(&IPv4Address{})

func (a *IPv4Address) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}

	ip := net.ParseIP(str)
	if ip == nil {
		return fmt.Errorf("%w: not an IPv4 address '%s'", ErrJSONUnmarshal, str)
	}

	a.IP = ip

	return nil
}

// Ensure the type implements value.UnstructuredConverter.
var _ = value.UnstructuredConverter(&IPv4Address{})

func (a *IPv4Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *IPv4Address) ToUnstructured() any {
	return a.String()
}

// There is no interface defined for these. See
// https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
// for reference.
func (*IPv4Address) OpenAPISchemaType() []string {
	return []string{"string"}
}

func (*IPv4Address) OpenAPISchemaFormat() string {
	return ""
}

// See https://regex101.com/r/QUfWrF/1
// +kubebuilder:validation:Type=string
// +kubebuilder:validation:Pattern="^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])\\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])\\/(?:3[0-2]|[1-2]?[0-9])$"
type IPv4Prefix struct {
	net.IPNet
}

// DeepCopyInto implements the interface deepcopy-gen is totally unable to
// do by itself.
func (p *IPv4Prefix) DeepCopyInto(out *IPv4Prefix) {
	if p.IP != nil {
		in, out := &p.IP, &out.IP
		*out = make(net.IP, len(*in))
		copy(*out, *in)
	}

	if p.Mask != nil {
		in, out := &p.Mask, &out.Mask
		*out = make(net.IPMask, len(*in))
		copy(*out, *in)
	}
}

// Ensure the type implements json.Unmarshaler.
var _ = json.Unmarshaler(&IPv4Prefix{})

func (p *IPv4Prefix) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}

	_, network, err := net.ParseCIDR(str)
	if err != nil {
		return fmt.Errorf("%w: not an IPv4 prefix '%s'", ErrJSONUnmarshal, str)
	}

	if network == nil {
		return fmt.Errorf("%w: not an IPv4 prefix '%s'", ErrJSONUnmarshal, str)
	}

	p.IPNet = *network

	return nil
}

// Ensure the type implements value.UnstructuredConverter.
var _ = value.UnstructuredConverter(&IPv4Prefix{})

func (p *IPv4Prefix) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

func (p *IPv4Prefix) ToUnstructured() any {
	return p.String()
}

// There is no interface defined for these. See
// https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
// for reference.
func (*IPv4Prefix) OpenAPISchemaType() []string {
	return []string{"string"}
}

func (*IPv4Prefix) OpenAPISchemaFormat() string {
	return ""
}

// Tag is an arbirary key/value.
type Tag struct {
	// Name of the tag.
	Name string `json:"name"`
	// Value of the tag.
	Value string `json:"value"`
}

// TagList is an ordered list of tags.
type TagList []Tag

// MachineGeneric contains common things across all machine pool types.
type MachineGeneric struct {
	// Image is the region service image to deploy with.
	ImageID string `json:"imageId"`
	// Flavor is the regions service flavor to deploy with.
	FlavorID string `json:"flavorId"`
	// DiskSize is the persistent root disk size to deploy with.  This
	// overrides the default ephemeral disk size defined in the flavor.
	// This is irrelevant for baremetal machine flavors.
	DiskSize *resource.Quantity `json:"diskSize,omitempty"`
	// Replicas is the initial pool size to deploy.
	// +kubebuilder:validation:Minimum=0
	Replicas int `json:"replicas,omitempty"`
}

// Network generic constains common networking options.
type NetworkGeneric struct {
	// NodeNetwork is the IPv4 prefix for the node network.
	// This is tyically used to populate a physical network address range.
	NodeNetwork IPv4Prefix `json:"nodeNetwork"`
	// DNSNameservers sets the DNS nameservers for hosts on the network.
	// +listType=set
	// +kubebuilder:validation:MinItems=1
	DNSNameservers []IPv4Address `json:"dnsNameservers"`
}

// +kubebuilder:validation:Enum=Available;Healthy
type ConditionType string

const (
	// ConditionAvailable if not defined or false means that the
	// resource is not ready, or is known to be in a bad state and should
	// not be used.  When true, while not guaranteed to be fully functional.
	ConditionAvailable ConditionType = "Available"
	// ConditionHealthy if defined describes the current healthiness of
	// the resource.
	ConditionHealthy ConditionType = "Healthy"
)

// ConditionReason defines the possible reasons of a resource
// condition.  These are generic and may be used by any condition.
// +kubebuilder:validation:Enum=Provisioning;Provisioned;Cancelled;Errored;Deprovisioning;Deprovisioned;Unknown;Healthy;Degraded
type ConditionReason string

// Condition reasons for ConditionAvailable.
const (
	// ConditionReasonProvisioning is used for the Available condition
	// to indicate that a resource has been seen, it has no pre-existing condition
	// and we assume it's being provisioned for the first time.
	ConditionReasonProvisioning ConditionReason = "Provisioning"
	// ConditionReasonProvisioned is used for the Available condition
	// to mean that the resource is ready to be used.
	ConditionReasonProvisioned ConditionReason = "Provisioned"
	// ConditionReasonCancelled is used by a condition to
	// indicate the controller was cancelled e.g. via a container shutdown.
	ConditionReasonCancelled ConditionReason = "Cancelled"
	// ConditionReasonErrored is used by a condition to
	// indicate an unexpected error occurred e.g. Kubernetes API transient error.
	// If we see these, consider formulating a fix, for example a retry loop.
	ConditionReasonErrored ConditionReason = "Errored"
	// ConditionReasonDeprovisioning is used by a condition to
	// indicate the controller has picked up a deprovision event.
	ConditionReasonDeprovisioning ConditionReason = "Deprovisioning"
	// ConditionReasonDeprovisioned is used by a condition to
	// indicate we have finished deprovisioning and the Kubernetes
	// garbage collector can remove the resource.
	ConditionReasonDeprovisioned ConditionReason = "Deprovisioned"
)

// Condition reasons for ConditionHealthy.
const (
	// ConditionReasonUnknown means the health status cannot be derived.
	ConditionReasonUnknown ConditionReason = "Unknown"
	// ConditionReasonHealthy means all subresources associated with the
	// resource are in a healthy state.
	ConditionReasonHealthy ConditionReason = "Healthy"
	// ConditionReasonDegraded means some subresources associated with the
	// resource are degraded e.g. a deployment not correctly scaled etc.
	ConditionReasonDegraded ConditionReason = "Degraded"
)

// Condition is a generic condition type for use across all resource types.
// It's generic so that the underlying controller-manager functionality can
// be shared across all resources.
type Condition struct {
	// Type is the type of the condition.
	Type ConditionType `json:"type"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	Reason ConditionReason `json:"reason"`
	// Human-readable message indicating details about last transition.
	Message string `json:"message"`
}

// ApplicationReferenceKind defines the application kind we wish to reference.
type ApplicationReferenceKind string

const (
	// ApplicationReferenceKindHelm references a helm application.
	ApplicationReferenceKindHelm ApplicationReferenceKind = "HelmApplication"
)

type ApplicationReference struct {
	// Kind is the kind of resource we are referencing.
	// +kubebuilder:validation:Enum=HelmApplication
	Kind *ApplicationReferenceKind `json:"kind"`
	// Name is the name of the resource we are referencing.
	Name *string `json:"name"`
	// Version is the version of the application within the application type.
	Version SemanticVersion `json:"version"`
}

type ApplicationNamedReference struct {
	// Name is the name of the application.  This must match what is encoded into
	// Unikorn's application management engine.
	Name *string `json:"name"`
	// Reference is a reference to the application definition.
	Reference *ApplicationReference `json:"reference"`
}
