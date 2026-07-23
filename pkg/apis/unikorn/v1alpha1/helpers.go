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
	"errors"
	"net"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// ErrStatusConditionLookup is raised when a condition is not found in
	// the resource status.
	ErrStatusConditionLookup = errors.New("status condition not found")

	// ErrMissingLabel is raised when an expected label is not present on
	// a resource.
	ErrMissingLabel = errors.New("expected label is missing")

	// ErrApplicationLookup is raised when the named application is not
	// present in an application bundle bundle.
	ErrApplicationLookup = errors.New("failed to lookup an application")
)

// IPv4AddressSliceFromIPSlice is a simple converter from Go types
// to API types.
func IPv4AddressSliceFromIPSlice(in []net.IP) []IPv4Address {
	out := make([]IPv4Address, len(in))

	for i, ip := range in {
		out[i] = IPv4Address{IP: ip}
	}

	return out
}

// GetCondition is a generic condition lookup function.
func GetCondition(conditions []metav1.Condition, t ConditionType) (*metav1.Condition, error) {
	condition := meta.FindStatusCondition(conditions, string(t))
	if condition == nil {
		return nil, ErrStatusConditionLookup
	}

	return condition, nil
}

// UpdateCondition either adds or updates a condition in the resource status.
// The last transition time is only bumped when the status actually changes,
// per the standard metav1.Condition semantics.
func UpdateCondition(conditions *[]metav1.Condition, t ConditionType, status corev1.ConditionStatus, reason string, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:    string(t),
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	})
}

// TypedCondition allows the condition reasons of a condition to be narrowed.
// +k8s:deepcopy-gen=false
type TypedCondition[R ~string] struct {
	// Type is the type of the condition.
	Type ConditionType
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status corev1.ConditionStatus
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time
	// Unique, one-word, CamelCase reason for the condition's last transition.
	Reason R
	// Human-readable message indicating details about last transition.
	Message string
}

// GetTypedCondition reads a condition and narrows its reason to R. Core's own
// conditions have dedicated wrappers below (GetAvailableCondition, ...); this is
// exported so a domain can build the same typed accessor for a condition whose
// reason vocabulary it owns (e.g. a lifecycle Active condition), reusing the
// generic handling rather than re-casting a raw metav1.Condition.
func GetTypedCondition[R ~string](r StatusConditionReader, t ConditionType) (*TypedCondition[R], error) {
	condition, err := r.StatusConditionRead(t)
	if err != nil {
		return nil, err
	}

	out := &TypedCondition[R]{
		Type:               ConditionType(condition.Type),
		Status:             corev1.ConditionStatus(condition.Status),
		LastTransitionTime: condition.LastTransitionTime,
		Reason:             R(condition.Reason),
		Message:            condition.Message,
	}

	return out, nil
}

// GetAvailableCondition reads the Available condition, narrowing its reason to
// the provisioning vocabulary.
func GetAvailableCondition(r StatusConditionReader) (*TypedCondition[ProvisioningConditionReason], error) {
	return GetTypedCondition[ProvisioningConditionReason](r, ConditionAvailable)
}

// GetHealthyCondition reads the Healthy condition, narrowing its reason to the
// health vocabulary.
func GetHealthyCondition(r StatusConditionReader) (*TypedCondition[HealthConditionReason], error) {
	return GetTypedCondition[HealthConditionReason](r, ConditionHealthy)
}

// Contains returns if the k/v tag exists in the list.
func (t TagList) Contains(tag Tag) bool {
	return slices.ContainsFunc(t, func(temp Tag) bool {
		return temp.Name == tag.Name && temp.Value == tag.Value
	})
}

// ContainsAll returns if all k/v tags exist in the list.
func (t TagList) ContainsAll(o TagList) bool {
	for _, tag := range o {
		if !t.Contains(tag) {
			return false
		}
	}

	return true
}

func (t TagList) Find(name string) (string, bool) {
	predicate := func(tag Tag) bool {
		return tag.Name == name
	}

	index := slices.IndexFunc(t, predicate)
	if index < 0 {
		return "", false
	}

	return t[index].Value, true
}
