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

package application

import (
	"context"
	"errors"
	"slices"

	unikornv1 "github.com/unikorn-cloud/core/pkg/apis/unikorn/v1alpha1"
	"github.com/unikorn-cloud/core/pkg/cd"
	clientlib "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/provisioners"
	"github.com/unikorn-cloud/core/pkg/provisioners/remotecluster"
	"github.com/unikorn-cloud/core/pkg/util"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Provisioner deploys an application that is keyed to a specific resource.
// For example, ArgoCD dictates that applications be installed in the same
// namespace, so we use the resource to define a unique set of labels that
// identifies that resource out of all others, and add in the application
// name to uniquely identify the application within that resource.
type Provisioner struct {
	// Metadata defines the application name, this directly affects
	// the application what will be searched for in the application bundle
	// defined in the resource.  It will also be the default Application ID
	// name, unless overridden by Name.
	provisioners.Metadata

	// namespace explicitly sets the namespace for the application.
	namespace string

	// generator provides application generation functionality.
	generator any

	// allowDegraded accepts a degraded status as a success for an application.
	allowDegraded bool

	// applicationGetter is responsible for fetching an application.
	applicationGetter GetterFunc

	// applicationVersion is a reference to a versioned application.
	applicationVersion *unikornv1.HelmApplicationVersion
}

// New returns a new initialized provisioner object.
func New(applicationGetter GetterFunc) *Provisioner {
	return &Provisioner{
		applicationGetter: applicationGetter,
	}
}

// Ensure the Provisioner interface is implemented.
var _ provisioners.Provisioner = &Provisioner{}

// InNamespace deploys the application into an explicit namespace.
func (p *Provisioner) InNamespace(namespace string) *Provisioner {
	p.namespace = namespace

	return p
}

// WithGenerator registers an object that can generate implicit configuration where
// you cannot do it all from the default set of arguments.
func (p *Provisioner) WithGenerator(generator any) *Provisioner {
	p.generator = generator

	return p
}

// AllowDegraded accepts a degraded status as a success for an application.
func (p *Provisioner) AllowDegraded() *Provisioner {
	p.allowDegraded = true

	return p
}

func (p *Provisioner) getResourceID(ctx context.Context) (*cd.ResourceIdentifier, error) {
	id := &cd.ResourceIdentifier{
		Name: p.Name,
	}

	l, err := FromContext(ctx).ResourceLabels()
	if err != nil {
		return nil, err
	}

	if len(l) > 0 {
		id.Labels = make([]cd.ResourceIdentifierLabel, 0, len(l))

		// Make label ordering deterministic for the sake of testing...
		k := util.Keys(l)
		slices.Sort(k)

		for _, key := range k {
			id.Labels = append(id.Labels, cd.ResourceIdentifierLabel{
				Name:  key,
				Value: l[key],
			})
		}
	}

	return id, nil
}

// getReleaseName uses the release name in the application spec by default
// but allows the generator to override it.
func (p *Provisioner) getReleaseName(ctx context.Context) string {
	var name string

	if p.applicationVersion.Release != nil {
		name = *p.applicationVersion.Release
	}

	if p.generator != nil {
		if releaseNamer, ok := p.generator.(ReleaseNamer); ok {
			override := releaseNamer.ReleaseName(ctx)

			if override != "" {
				name = override
			}
		}
	}

	return name
}

// getParameters constructs a full list of Helm parameters by taking those provided
// in the application spec, and appending any that the generator yields.
func (p *Provisioner) getParameters(ctx context.Context) ([]cd.HelmApplicationParameter, error) {
	parameters := make([]cd.HelmApplicationParameter, 0, len(p.applicationVersion.Parameters))

	for _, parameter := range p.applicationVersion.Parameters {
		parameters = append(parameters, cd.HelmApplicationParameter{
			Name:  parameter.Name,
			Value: parameter.Value,
		})
	}

	if p.generator != nil {
		if parameterizer, ok := p.generator.(Paramterizer); ok {
			p, err := parameterizer.Parameters(ctx, p.applicationVersion.Version)
			if err != nil {
				return nil, err
			}

			for name, value := range p {
				parameters = append(parameters, cd.HelmApplicationParameter{
					Name:  name,
					Value: value,
				})
			}
		}
	}

	// Makes gomock happy as "nil" != "[]foo{}".
	if len(parameters) == 0 {
		return nil, nil
	}

	return parameters, nil
}

// getValues delegates to the generator to get an option values.yaml file to
// pass to Helm.
func (p *Provisioner) getValues(ctx context.Context) (any, error) {
	if p.generator == nil {
		//nolint:nilnil
		return nil, nil
	}

	valuesGenerator, ok := p.generator.(ValuesGenerator)
	if !ok {
		//nolint:nilnil
		return nil, nil
	}

	values, err := valuesGenerator.Values(ctx, p.applicationVersion.Version)
	if err != nil {
		return nil, err
	}

	return values, nil
}

func (p *Provisioner) getNamespaceMetadata(ctx context.Context) (map[string]string, map[string]string, error) {
	if p.generator == nil {
		return nil, nil, nil
	}

	namespaceGen, ok := p.generator.(NamespaceLabeler)
	if !ok {
		return nil, nil, nil
	}

	labels, annotations, err := namespaceGen.NamespaceMetadata(ctx, p.applicationVersion.Version)
	if err != nil {
		return nil, nil, err
	}

	return labels, annotations, nil
}

// getClusterID returns the destination cluster name.
func (p *Provisioner) getClusterID(ctx context.Context) (*cd.ResourceIdentifier, error) {
	clusterContext, err := clientlib.ClusterFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return clusterContext.ID, nil
}

func (p *Provisioner) getNamespace() string {
	if p.namespace != "" {
		return p.namespace
	}

	if p.applicationVersion.Namespace != nil {
		return *p.applicationVersion.Namespace
	}

	return "default"
}

// generateApplication converts the provided object to a canonical form for a driver.
//
//nolint:cyclop
func (p *Provisioner) generateApplication(ctx context.Context) (*cd.HelmApplication, error) {
	parameters, err := p.getParameters(ctx)
	if err != nil {
		return nil, err
	}

	values, err := p.getValues(ctx)
	if err != nil {
		return nil, err
	}

	clusterID, err := p.getClusterID(ctx)
	if err != nil {
		return nil, err
	}

	cdApplication := &cd.HelmApplication{
		Repo:          *p.applicationVersion.Repo,
		Version:       p.applicationVersion.Version.Original(),
		Release:       p.getReleaseName(ctx),
		Parameters:    parameters,
		Values:        values,
		Cluster:       clusterID,
		Namespace:     p.getNamespace(),
		AllowDegraded: p.allowDegraded,
	}

	if p.applicationVersion.Chart != nil {
		cdApplication.Chart = *p.applicationVersion.Chart
	}

	if p.applicationVersion.Branch != nil {
		cdApplication.Branch = *p.applicationVersion.Branch
	}

	if p.applicationVersion.Path != nil {
		cdApplication.Path = *p.applicationVersion.Path
	}

	if p.applicationVersion.CreateNamespace != nil {
		cdApplication.CreateNamespace = *p.applicationVersion.CreateNamespace
	}

	nsLabels, nsAnnotations, err := p.getNamespaceMetadata(ctx)
	if err != nil {
		return nil, err
	}

	cdApplication.NamespaceMetadata.Labels = nsLabels
	cdApplication.NamespaceMetadata.Annotations = nsAnnotations

	if p.applicationVersion.ServerSideApply != nil {
		cdApplication.ServerSideApply = *p.applicationVersion.ServerSideApply
	}

	if p.generator != nil {
		if customization, ok := p.generator.(Customizer); ok {
			ignoredDifferences, err := customization.Customize(p.applicationVersion.Version)
			if err != nil {
				return nil, err
			}

			cdApplication.IgnoreDifferences = ignoredDifferences
		}
	}

	return cdApplication, nil
}

// initialize must be called in Provision/Deprovision to do the application
// resolution in a path that has an error handler (as opposed to a constructor).
func (p *Provisioner) initialize(ctx context.Context) error {
	application, version, err := p.applicationGetter(ctx)
	if err != nil {
		return err
	}

	p.Name = application.Labels[constants.NameLabel]

	applicationVersion, err := application.GetVersion(*version)
	if err != nil {
		return err
	}

	p.applicationVersion = applicationVersion

	return nil
}

// Provision implements the Provision interface.
func (p *Provisioner) Provision(ctx context.Context) error {
	log := log.FromContext(ctx)

	if err := p.initialize(ctx); err != nil {
		return err
	}

	log.V(1).Info("provisioning application", "application", p.Name)

	// Convert the generic object type into what's expected by the driver interface.
	id, err := p.getResourceID(ctx)
	if err != nil {
		return err
	}

	application, err := p.generateApplication(ctx)
	if err != nil {
		return err
	}

	if err := cd.FromContext(ctx).CreateOrUpdateHelmApplication(ctx, id, application); err != nil {
		if errors.Is(err, provisioners.ErrYield) {
			log.Info("awaiting application provisioning", "application", p.Name)
		}

		return err
	}

	log.V(1).Info("application provisioned", "application", p.Name)

	if p.generator != nil {
		if hook, ok := p.generator.(PostProvisionHook); ok {
			if err := hook.PostProvision(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// Deprovision implements the Provision interface.
func (p *Provisioner) Deprovision(ctx context.Context) error {
	log := log.FromContext(ctx)

	if p.generator != nil {
		if hook, ok := p.generator.(PreDeprovisionHook); ok {
			if err := hook.PreDeprovision(ctx); err != nil {
				return err
			}
		}
	}

	if err := p.initialize(ctx); err != nil {
		return err
	}

	log.V(1).Info("deprovisioning application", "application", p.Name)

	id, err := p.getResourceID(ctx)
	if err != nil {
		return err
	}

	if err := cd.FromContext(ctx).DeleteHelmApplication(ctx, id, remotecluster.BackgroundDeletionFromContext(ctx)); err != nil {
		return err
	}

	return nil
}
