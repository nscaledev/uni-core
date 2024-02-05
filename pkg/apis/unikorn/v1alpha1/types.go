/*
Copyright 2022-2024 EscherCloud.
Copyright 2024 the Unikorn Authors.

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
	"net"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/structured-merge-diff/v4/value"
)

var (
	ErrJSONUnmarshal = errors.New("failed to unmarshal JSON")
)

// +kubebuilder:validation:Pattern="^v(?:[0-9]+\\.){2}(?:[0-9]+)$"
type SemanticVersion string

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
		return ErrJSONUnmarshal
	}

	a.IP = ip

	return nil
}

// Ensure the type implements value.UnstructuredConverter.
var _ = value.UnstructuredConverter(&IPv4Address{})

func (a IPv4Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.IP.String())
}

func (a IPv4Address) ToUnstructured() interface{} {
	return a.IP.String()
}

// There is no interface defined for these. See
// https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
// for reference.
func (IPv4Address) OpenAPISchemaType() []string {
	return []string{"string"}
}

func (IPv4Address) OpenAPISchemaFormat() string {
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
	if p.IPNet.IP != nil {
		in, out := &p.IPNet.IP, &out.IPNet.IP
		*out = make(net.IP, len(*in))
		copy(*out, *in)
	}

	if p.IPNet.Mask != nil {
		in, out := &p.IPNet.Mask, &out.IPNet.Mask
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
		return ErrJSONUnmarshal
	}

	if network == nil {
		return ErrJSONUnmarshal
	}

	p.IPNet = *network

	return nil
}

// Ensure the type implements value.UnstructuredConverter.
var _ = value.UnstructuredConverter(&IPv4Prefix{})

func (p IPv4Prefix) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.IPNet.String())
}

func (p IPv4Prefix) ToUnstructured() interface{} {
	return p.IP.String()
}

// There is no interface defined for these. See
// https://github.com/kubernetes/kube-openapi/tree/master/pkg/generators
// for reference.
func (IPv4Prefix) OpenAPISchemaType() []string {
	return []string{"string"}
}

func (IPv4Prefix) OpenAPISchemaFormat() string {
	return ""
}

// +kubebuilder:validation:Enum=Available
type ConditionType string

const (
	// ConditionAvailable if not defined or false means that the
	// resource is not ready, or is known to be in a bad state and should
	// not be used.  When true, while not guaranteed to be fully functional.
	ConditionAvailable ConditionType = "Available"
)

// ConditionReason defines the possible reasons of a resource
// condition.  These are generic and may be used by any condition.
// +kubebuilder:validation:Enum=Provisioning;Provisioned;Cancelled;Errored;Deprovisioning;Deprovisioned
type ConditionReason string

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
	// TODO: make mandatory.
	Version *string `json:"version,omitempty"`
}

type ApplicationNamedReference struct {
	// Name is the name of the application.  This must match what is encoded into
	// Unikorn's application management engine.
	Name *string `json:"name"`
	// Reference is a reference to the application definition.
	Reference *ApplicationReference `json:"reference"`
}
