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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HelmApplicationList defines a list of Helm applications.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type HelmApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmApplication `json:"items"`
}

// HelmApplication defines a Helm application.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Namespaced,categories=unikorn
// +kubebuilder:printcolumn:name="display name",type="string",JSONPath=".metadata.labels['unikorn-cloud\\.org/name']"
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp"
type HelmApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HelmApplicationSpec   `json:"spec"`
	Status            HelmApplicationStatus `json:"status,omitempty"`
}

type HelmApplicationSpec struct {
	// Tags are aribrary user data.
	Tags TagList `json:"tags,omitempty"`
	// Documentation defines a URL to 3rd party documentation.
	Documentation *string `json:"documentation"`
	// License describes the licence the application is released under.
	License *string `json:"license"`
	// Icon is a base64 encoded icon for the application.
	Icon []byte `json:"icon"`
	// Versions are the application versions that are supported.
	Versions []HelmApplicationVersion `json:"versions,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.chart) || has(self.branch)",message="either chart or branch must be specified"
// +kubebuilder:validation:XValidation:rule="!(has(self.chart) && has(self.branch))",message="only one of chart or branch may be specified"
type HelmApplicationVersion struct {
	// Repo is either a Helm chart repository, or git repository.
	Repo *string `json:"repo"`
	// Chart is the chart name in the repository.
	Chart *string `json:"chart,omitempty"`
	// Branch defines the branch name if the repo is a git repository.
	Branch *string `json:"branch,omitempty"`
	// Path is the path if the repo is a git repository.
	Path *string `json:"path,omitempty"`
	// Version is the chart version, but must also be set for Git based repositories.
	// This value must be a semantic version.
	Version SemanticVersion `json:"version"`
	// Release is the explicit release name for when chart resource names are dynamic.
	// Typically we need predicatable names for things that are going to be remote
	// clusters to derive endpoints or Kubernetes configurations.
	// If not set, uses the application default.
	Release *string `json:"release,omitempty"`
	// Parameters is a set of static --set parameters to pass to the chart.
	// If not set, uses the application default.
	Parameters []HelmApplicationParameter `json:"parameters,omitempty"`
	// Namespace is the namespace to install the application to.
	Namespace *string `json:"namespace,omitempty"`
	// CreateNamespace indicates whether the chart requires a namespace to be
	// created by the tooling, rather than the chart itself.
	// If not set, uses the application default.
	CreateNamespace *bool `json:"createNamespace,omitempty"`
	// ServerSideApply allows you to bypass using kubectl apply.  This is useful
	// in situations where CRDs are too big and blow the annotation size limit.
	// We'd like to have this on by default, but mutating admission webhooks and
	// controllers modifying the spec mess this up.
	// If not set, uses the application default.
	ServerSideApply *bool `json:"serverSideApply,omitempty"`
	// Dependencies capture hard dependencies on other applications that must
	// be installed before this one.
	Dependencies []HelmApplicationDependency `json:"dependencies,omitempty"`
	// Recommends capture soft dependencies on other applications that may be
	// installed after this one. Typically ths could be storage classes for a
	// storage provider etc.
	Recommends []HelmApplicationRecommendation `json:"recommends,omitempty"`
}

type HelmApplicationParameter struct {
	// Name is the name of the parameter.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Value is the value of the parameter.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

type HelmApplicationDependency struct {
	// Name of the application to depend on.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Constraints is a set of versioning constraints that must be met
	// by a SAT solver.
	Constraints *SemanticVersionConstraints `json:"constraints,omitempty"`
}

type HelmApplicationRecommendation struct {
	// Name of the application to require.
	// That recommendation MUST have a dependency with any constraints
	// on this application.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

type HelmApplicationStatus struct{}
