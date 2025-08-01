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

package application

import (
	"context"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/cd"
)

// ApplicationGetter abstracts away how an application is looked up for a
// specific entity.
type GetterFunc func(ctx context.Context) (*unikornv1.HelmApplication, *unikornv1.SemanticVersion, error)

// ReleaseNamer is an interface that allows generators to supply an implicit release
// name to Helm.
type ReleaseNamer interface {
	ReleaseName(ctx context.Context) string
}

// Paramterizer is an interface that allows generators to supply a list of parameters
// to Helm.  These are in addition to those defined by the application template.  At
// present, there is nothing special about overriding, it just appends, so ensure the
// explicit and implicit sets don't overlap.
type Paramterizer interface {
	Parameters(ctx context.Context, version unikornv1.SemanticVersion) (map[string]string, error)
}

// ValuesGenerator is an interface that allows generators to supply a raw values.yaml
// file to Helm.  This accepts an object that can be marshaled to YAML.
type ValuesGenerator interface {
	Values(ctx context.Context, version unikornv1.SemanticVersion) (any, error)
}

// NamespaceLabeler is an interface you can implement in a generator, to give a namespace created
// by the CD labels and annotations.
type NamespaceLabeler interface {
	NamespaceMetadata(ctx context.Context, version unikornv1.SemanticVersion) (map[string]string, map[string]string, error)
}

// Customizer is a generic generator interface that implemnets raw customizations to
// the application template.  Try to avoid using this.
type Customizer interface {
	Customize(version unikornv1.SemanticVersion) ([]cd.HelmApplicationField, error)
}

// PostProvisionHook is an interface that lets an application provisioner run
// a callback when provisioning has completed successfully.
type PostProvisionHook interface {
	PostProvision(ctx context.Context) error
}

// PreDeprovisionHook is an interface that lets an application deprovisioner run
// a callback before deprovisioning an application e.g. to handle manual resource
// deletion.
type PreDeprovisionHook interface {
	PreDeprovision(ctx context.Context) error
}
