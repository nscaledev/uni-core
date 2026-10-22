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

//go:generate mockgen -source=interfaces.go -destination=mock/interfaces.go -package=mock

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	StatusConditionRead(t ConditionType) (*metav1.Condition, error)
}

// GenerationProcessor is implemented by resources that record the last spec
// generation their controller finished with.  Opt in per type, asserted at
// runtime, deliberately not part of ManagableResourceInterface.
//
// The manager stamps it on a successful provision only; any failure, yield or
// recheck, and any deprovision, leaves it as it was.  It is a record for the
// provisioner to read inside a reconcile, never an event filter: see the
// manager README's processed generation guard rail.
//
// Zero means never processed.  A live resource starts at generation 1, so the
// zero value correctly reads as outstanding work.
type GenerationProcessor interface {
	// ProcessedGeneration returns the last spec generation the controller
	// finished with, or zero if it has never finished one.
	ProcessedGeneration() int64

	// SetProcessedGeneration records a generation as finished with.
	SetProcessedGeneration(generation int64)
}

// ProvisioningConditionWriter sets the Available condition, with its reason
// constrained to the provisioning vocabulary.  Every managed resource must
// implement it; it is how the manager reports provisioning progress.
type ProvisioningConditionWriter interface {
	SetProvisioningCondition(status corev1.ConditionStatus, reason ProvisioningConditionReason, message string)
}

// HealthConditionWriter sets the Healthy condition, with its reason constrained
// to the health vocabulary.  It is optional: only resources that compute a
// health status implement it, and callers type-assert for it.
type HealthConditionWriter interface {
	SetHealthCondition(status corev1.ConditionStatus, reason HealthConditionReason, message string)
}

// ManagableResourceInterface is a resource type that can be manged e.g. has a
// controller associateds with it.
type ManagableResourceInterface interface {
	client.Object
	ResourceLabeller
	ReconcilePauser
	StatusConditionReader
	ProvisioningConditionWriter
}
