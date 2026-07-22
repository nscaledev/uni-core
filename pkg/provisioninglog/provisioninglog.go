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

// Package provisioninglog emits a structured, tenant-scoped stream of
// provisioning-status transitions. It mirrors the identity audit-log pattern —
// the discriminator is the structured-log message (filter on
// msg == "provisioning"), the payload is typed structs as key/values — but is
// cross-cutting: it is emitted from the reconcile path for every managed
// resource, so it must be driven edge-triggered by the caller (emit only when a
// condition actually changes, never on every poll).
package provisioninglog

import (
	"context"

	"github.com/unikorn-cloud/core/pkg/constants"

	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// discriminator is the structured-log message that marks a provisioning event, in
// the same way identity's audit middleware uses the message "audit". A log
// pipeline filters the stream on msg == "provisioning".
const discriminator = "provisioning"

// scopeLabels maps the envelope's scope keys to the platform label keys they are
// sourced from. It is deliberately a present-only projection of whatever tenancy
// labels the resource actually carries: a project-scoped resource emits both, an
// organization-scoped one (e.g. an image or snapshot) emits only organization,
// and a future folder/graph hierarchy attaches via new labels that are added
// here — no envelope shape change, no consumer break. Core owns the label
// mechanism; it deliberately does not model identity's hierarchy semantics.
var scopeLabels = map[string]string{
	"organization": constants.OrganizationLabel,
	"project":      constants.ProjectLabel,
}

// Component identifies the emitting service. Field names match the audit log's
// Component for parity.
type Component struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Resource identifies the resource whose provisioning state changed. Field names
// match the audit log's Resource for parity.
type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Provisioning carries the transition itself: the Available condition's status,
// its closed-vocabulary reason, and the user-safe message (the "why").
type Provisioning struct {
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// Emit writes a single provisioning-transition event. The caller is responsible
// for edge-triggering — only calling Emit when the (status, reason, message)
// tuple has actually changed — since the reconcile path runs on every poll.
func Emit(ctx context.Context, scheme *runtime.Scheme, object client.Object, status, reason, message string) {
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

	log.FromContext(ctx).Info(discriminator,
		"component", &Component{
			Name: group,
		},
		"resource", &Resource{
			Type: kind,
			ID:   object.GetName(),
		},
		"scope", scope,
		"provisioning", &Provisioning{
			Status:  status,
			Reason:  reason,
			Message: message,
		},
		"observedGeneration", object.GetGeneration(),
	)
}
