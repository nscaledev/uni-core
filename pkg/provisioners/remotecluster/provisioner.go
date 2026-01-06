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

package remotecluster

import (
	"context"
	goerrors "errors"
	"fmt"
	"net"
	"net/url"
	"sync"

	"github.com/unikorn-cloud/core/pkg/cd"
	clientlib "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/errors"
	"github.com/unikorn-cloud/core/pkg/provisioners"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RemoteCluster provides generic handling of remote cluster instances.
// Specialization is delegated to a provider specific interface.
type RemoteCluster struct {
	// generator provides a method to derive cluster names and configuration.
	generator provisioners.RemoteCluster

	// controller tells whether we "own" this resource or not.
	controller bool

	// lock provides synchronization around concurrrency.
	lock sync.Mutex

	// refCount tells us how many remote provisioners have been registered.
	refCount int

	// currentCount tells us how many times remote provisioners have been called.
	currentCount int
}

// New returns a new initialized provisioner object.
func New(generator provisioners.RemoteCluster, controller bool) *RemoteCluster {
	return &RemoteCluster{
		generator:  generator,
		controller: controller,
	}
}

// remoteClusterProvisioner is created when we want to run a provisioner on a remote
// cluster.
type remoteClusterProvisioner struct {
	provisioners.Metadata

	// provisioner is a reference to the remote cluster, it contains global
	// information about the remote cluster that this provisioner is operating
	// on.
	remote *RemoteCluster

	// child is the provisioner to run on the remote cluster.
	child provisioners.Provisioner

	// backgroundDeletion, if set, is propagated to descendant provisioners via
	// the context.  At present, this is only available on remote provisioners,
	// and is intended to be used for quickly discarding applications on dynamically
	// provisioned clusters that will be destroyed anyway.  The one caveat is that
	// it cannot be used with remotes where applications need to be given a chance to
	// clean up resources that will be orphaned.
	backgroundDeletion bool

	// prefix is an optional secret prefix to avoid collisions when the
	// same remote os defined in different contexts e.g. create a cluster,
	// import that cluster as a region.
	prefix string
}

// Ensure the Provisioner interface is implemented.
var _ provisioners.Provisioner = &remoteClusterProvisioner{}

// Allows us to specify options for the provided provisioner.
type ProvisionerOption func(p *remoteClusterProvisioner)

func BackgroundDeletion(p *remoteClusterProvisioner) {
	p.backgroundDeletion = true
}

func WithPrefix(prefix string) func(*remoteClusterProvisioner) {
	return func(p *remoteClusterProvisioner) {
		p.prefix = prefix
	}
}

// GetClient gets a client from the remote generator.
// NOTE: this must only be called in Provision/Deprovision so it
// respects the context we are in as regards nested remotes.
func (r *RemoteCluster) getClient(ctx context.Context) (client.Client, *clientcmdapi.Config, error) {
	config, err := r.generator.Config(ctx)
	if err != nil {
		return nil, nil, err
	}

	getter := func() (*clientcmdapi.Config, error) {
		return config, nil
	}

	restConfig, err := clientcmd.BuildConfigFromKubeconfigGetter("", getter)
	if err != nil {
		return nil, nil, err
	}

	client, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, nil, err
	}

	return client, config, nil
}

// ProvisionOn returns a provisioner that will provision the remote,
// and provision the child provisioner on that remote.
func (r *RemoteCluster) ProvisionOn(child provisioners.Provisioner, options ...ProvisionerOption) provisioners.Provisioner {
	r.refCount++

	provisioner := &remoteClusterProvisioner{
		Metadata: provisioners.Metadata{
			Name: "remote-cluster",
		},
		remote: r,
		child:  child,
	}

	for _, o := range options {
		o(provisioner)
	}

	return provisioner
}

func (p *remoteClusterProvisioner) provisionRemote(ctx context.Context) error {
	log := log.FromContext(ctx)

	p.remote.lock.Lock()
	defer p.remote.lock.Unlock()

	p.remote.currentCount++

	id := p.remote.generator.ID()

	// If this is the first remote cluster encountered, reconcile it.
	if p.remote.controller && p.remote.currentCount == 1 {
		log.V(1).Info("provisioning remote cluster", "remotecluster", id)

		config, err := p.remote.generator.Config(ctx)
		if err != nil {
			return err
		}

		cluster := &cd.Cluster{
			Config: config,
			Prefix: p.prefix,
		}

		if err := cd.FromContext(ctx).CreateOrUpdateCluster(ctx, id, cluster); err != nil {
			log.V(1).Info("remote cluster not ready, yielding", "remotecluster", id)

			return provisioners.ErrYield
		}

		log.V(1).Info("remote cluster provisioned", "remotecluster", id)
	}

	return nil
}

func getKuebernetesURL(config *clientcmdapi.Config) (*url.URL, error) {
	configContext := config.Contexts[config.CurrentContext]
	if configContext == nil {
		return nil, fmt.Errorf("%w: unable to lookup context", errors.ErrKubeconfig)
	}

	cluster := config.Clusters[configContext.Cluster]
	if cluster == nil {
		return nil, fmt.Errorf("%w: unable to lookup cluster", errors.ErrKubeconfig)
	}

	return url.Parse(cluster.Server)
}

func getHostPort(url *url.URL) (string, string) {
	host, port, err := net.SplitHostPort(url.Host)
	if err != nil {
		host = url.Host

		switch url.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}

	return host, port
}

// Provision implements the Provision interface.
func (p *remoteClusterProvisioner) Provision(ctx context.Context) error {
	if err := p.provisionRemote(ctx); err != nil {
		return err
	}

	client, config, err := p.remote.getClient(ctx)
	if err != nil {
		return err
	}

	url, err := getKuebernetesURL(config)
	if err != nil {
		return err
	}

	host, port := getHostPort(url)

	clusterContext := &clientlib.ClusterContext{
		Client: client,
		ID:     p.remote.generator.ID(),
		Host:   host,
		Port:   port,
	}

	ctx = clientlib.NewContextWithCluster(ctx, clusterContext)

	// Remote is registered, create the remote applications.
	if err := p.child.Provision(ctx); err != nil {
		return err
	}

	return nil
}

// Deprovision implements the Provision interface.
func (p *remoteClusterProvisioner) Deprovision(ctx context.Context) error {
	log := log.FromContext(ctx)

	// If the client cannot be instantiated due to a yield error, then
	// assume the client config is gone, and the child deprovisioning
	// has completed successfully.
	deprovisioned := false

	client, config, err := p.remote.getClient(ctx)
	if err != nil {
		if !goerrors.Is(err, provisioners.ErrYield) {
			return err
		}

		deprovisioned = true
	}

	if !deprovisioned {
		url, err := getKuebernetesURL(config)
		if err != nil {
			return err
		}

		host, port := getHostPort(url)

		clusterContext := &clientlib.ClusterContext{
			Client: client,
			ID:     p.remote.generator.ID(),
			Host:   host,
			Port:   port,
		}

		ctx = clientlib.NewContextWithCluster(ctx, clusterContext)

		if p.backgroundDeletion {
			ctx = NewContextWithBackgroundDeletion(ctx, true)
		}

		if err := p.child.Deprovision(ctx); err != nil {
			return err
		}
	}

	// Once all concurrent remote provisioner have done there stuff
	// they will wait on the lock...
	p.remote.lock.Lock()
	defer p.remote.lock.Unlock()

	// ... adding themselves to the total...
	p.remote.currentCount++

	id := p.remote.generator.ID()

	// ... and if all have completed without an error, then deprovision the
	// remote cluster itself.
	if p.remote.controller && p.remote.currentCount == p.remote.refCount {
		log.V(1).Info("deprovisioning remote cluster", "remotecluster", id)

		if err := cd.FromContext(ctx).DeleteCluster(ctx, id); err != nil {
			return err
		}

		log.V(1).Info("remote cluster deprovisioned", "remotecluster", id)
	}

	return nil
}
