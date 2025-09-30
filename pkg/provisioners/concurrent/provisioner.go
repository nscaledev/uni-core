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

package concurrent

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/unikorn-cloud/core/pkg/provisioners"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Provisioner struct {
	provisioners.Metadata

	// provisioners is the set of provisions to provision
	// concurrently.
	provisioners []provisioners.Provisioner
}

func New(name string, p ...provisioners.Provisioner) *Provisioner {
	return &Provisioner{
		Metadata: provisioners.Metadata{
			Name: name,
		},
		provisioners: p,
	}
}

// Ensure the Provisioner interface is implemented.
var _ provisioners.Provisioner = &Provisioner{}

// Provision implements the Provision interface.
//
//nolint:dupl
func (p *Provisioner) Provision(ctx context.Context) error {
	log := log.FromContext(ctx)

	log.V(1).Info("provisioning concurrency group", "group", p.Name)

	group := &errgroup.Group{}

	for i := range p.provisioners {
		provisioner := p.provisioners[i]

		callback := func() error {
			// As errgroup only saves the first error, we may lose some
			// logging information, so do this here when waiting on child
			// tasks.
			if err := provisioner.Provision(ctx); err != nil {
				log.V(1).Info("concurrency group member exited with error", "error", err, "group", p.Name, "provisioner", provisioner.ProvisionerName())

				return err
			}

			return nil
		}

		group.Go(callback)
	}

	if err := group.Wait(); err != nil {
		log.V(1).Info("concurrency group provision failed", "group", p.Name)

		return err
	}

	log.V(1).Info("concurrency group provisioned", "group", p.Name)

	return nil
}

// Deprovision implements the Provision interface.
//
//nolint:dupl
func (p *Provisioner) Deprovision(ctx context.Context) error {
	log := log.FromContext(ctx)

	log.V(1).Info("deprovisioning concurrency group", "group", p.Name)

	group := &errgroup.Group{}

	for i := range p.provisioners {
		provisioner := p.provisioners[i]

		callback := func() error {
			// As errgroup only saves the first error, we may lose some
			// logging information, so do this here when waiting on child
			// tasks.
			if err := provisioner.Deprovision(ctx); err != nil {
				log.V(1).Info("concurrency group member exited with error", "error", err, "group", p.Name, "provisioner", provisioner.ProvisionerName())

				return err
			}

			return nil
		}

		group.Go(callback)
	}

	if err := group.Wait(); err != nil {
		log.V(1).Info("concurrency group deprovision failed", "group", p.Name)

		return err
	}

	log.V(1).Info("concurrency group deprovisioned", "group", p.Name)

	return nil
}
