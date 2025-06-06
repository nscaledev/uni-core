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

package v1alpha1

//go:generate mockgen -source=interfaces.go -destination=mock/interfaces.go -package=mock

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceLabeller is a generic interface over all resource types,
// where the resource can be uniquely identified.  As these typically map to
// custom resource types, be extra careful you don't overload anything in
// metav1.Object or runtime.Object.
type ResourceLabeller interface {
	// ResourceLabels returns a set of labels from the resource that uniquely
	// identify it, if they all were to reside in the same namespace.
	// In database terms this would be a composite key.
	ResourceLabels() (labels.Set, error)
}

// ReconcilePauser indicates a resource can have its reconciliation paused.
type ReconcilePauser interface {
	// Paused indicates a resource is paused and will not do anything.
	Paused() bool
}

// StatusConditionReader allows generic status conditions to be read.
type StatusConditionReader interface {
	// StatusConditionRead scans the status conditions for an existing condition
	// whose type matches.
	StatusConditionRead(t ConditionType) (*Condition, error)
}

// StatusConditionWriter allows generic status conditions to be updated.
type StatusConditionWriter interface {
	// StatusConditionWrite either adds or updates a condition in the resource
	// status. If the condition, status and message match an existing condition
	// the update is ignored.
	StatusConditionWrite(t ConditionType, status corev1.ConditionStatus, reason ConditionReason, message string)
}

// ManagableResourceInterface is a resource type that can be manged e.g. has a
// controller associateds with it.
type ManagableResourceInterface interface {
	client.Object
	ResourceLabeller
	ReconcilePauser
	StatusConditionReader
	StatusConditionWriter
}
