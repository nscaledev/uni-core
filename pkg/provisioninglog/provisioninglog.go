/*
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

// Package provisioninglog emits structured, tenant-scoped streams of
// condition-status transitions. It mirrors the identity audit-log pattern — the
// discriminator is the structured-log message, the payload is typed structs as
// key/values — but is cross-cutting: it is driven edge-triggered by the caller
// (emit only when a condition actually changes, never on every poll).
//
// Each condition axis is its own stream with its own message discriminator so a
// pipeline can subscribe per axis (filter on msg == "provisioning" for the
// Available/provisioning axis, msg == "lifecycle" for the Active/lifecycle-power
// axis). The envelope is otherwise identical across streams, so every axis has
// parity: same component/resource/scope/observedGeneration and the same payload
// shape, keyed by the stream name.
package provisioninglog

import (
	"context"

	"github.com/unikorn-cloud/core/pkg/constants"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// A Stream is the structured-log message that marks a condition-transition event,
// in the same way identity's audit middleware uses the message "audit". A log
// pipeline filters a stream on msg == the chosen value. The stream name doubles
// as the payload key, so each axis carries its transition under its own key.
const (
	// StreamProvisioning carries Available-condition (provisioning) transitions.
	StreamProvisioning = "provisioning"
	// StreamLifecycle carries Active-condition (lifecycle/power) transitions.
	StreamLifecycle = "lifecycle"
)

// scopeLabels maps the envelope's scope keys to the platform label keys they are
// sourced from. It is deliberately a present-only projection of whatever tenancy
// labels the resource actually carries: a project-scoped resource emits both, an
// organization-scoped one (e.g. an image or snapshot) emits only organization,
// and a future folder/graph hierarchy attaches via new labels that are added
// here — no envelope shape change, no consumer break. Core owns the label
// mechanism; it deliberately does not model identity's hierarchy semantics.
//
//nolint:gochecknoglobals
var scopeLabels = map[string]string{
	"organization": constants.OrganizationLabel,
	"project":      constants.ProjectLabel,
}

// Component identifies the emitting service. Field names match the audit log's
// Component for parity.
type Component struct {
	Name string `json:"name"`
	// Version is unset today - service-version threading is a follow-up - and is
	// kept for parity with the audit log's Component.
	Version string `json:"version,omitempty"`
}

// Resource identifies the resource whose provisioning state changed. Field names
// match the audit log's Resource for parity.
type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Transition carries the condition change itself: the condition's status, its
// reason, and the user-safe message (the "why"). It is shared across streams so
// every axis has the same payload shape (parity).
type Transition struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// Emit writes a single condition-transition event to the named stream (use one of
// the Stream* discriminators). The caller is responsible for edge-triggering —
// only calling Emit when the (status, reason, message) tuple has actually changed
// — since the reconcile and monitor paths run on every poll. The stream name is
// both the message discriminator and the key its transition payload is logged
// under, so consumers of one axis are never confused by another's.
func Emit(ctx context.Context, scheme *runtime.Scheme, object client.Object, stream, status, reason, message string) {
	var kind, group string

	if gvks, _, err := scheme.ObjectKinds(object); err == nil && len(gvks) > 0 {
		kind = gvks[0].Kind
		group = gvks[0].Group
	}

	scope := map[string]string{}

	labels := object.GetLabels()

	for key, label := range scopeLabels {
		if v, ok := labels[label]; ok && v != "" {
			scope[key] = v
		}
	}

	log.FromContext(ctx).Info(stream,
		"component", &Component{
			Name: group,
		},
		"resource", &Resource{
			Type: kind,
			ID:   object.GetName(),
		},
		"scope", scope,
		stream, &Transition{
			Status:  status,
			Reason:  reason,
			Message: message,
		},
		"observedGeneration", object.GetGeneration(),
	)
}
