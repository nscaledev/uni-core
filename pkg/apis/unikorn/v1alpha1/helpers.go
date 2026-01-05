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
func GetCondition(conditions []Condition, t ConditionType) (*Condition, error) {
	for i, condition := range conditions {
		if condition.Type == t {
			return &conditions[i], nil
		}
	}

	return nil, ErrStatusConditionLookup
}

// UpdateCondition either adds or updates a condition in the resource
// status. If the condition, status and message match an existing condition
// the update is ignored.
func UpdateCondition(conditions *[]Condition, t ConditionType, status corev1.ConditionStatus, reason ConditionReason, message string) {
	condition := Condition{
		Type:               t,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	existingPtr, err := GetCondition(*conditions, t)
	if err != nil {
		*conditions = append(*conditions, condition)

		return
	}

	// Do a shallow copy and set the same time, then do a shallow equality to
	// see if we need an update.
	existing := *existingPtr
	existing.LastTransitionTime = condition.LastTransitionTime

	if existing != condition {
		*existingPtr = condition
	}
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
