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

package manager

import (
	"context"
	"os"

	"github.com/spf13/pflag"

	coreclient "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/util"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ControllerOptions abstracts controller specific flags.
type ControllerOptions interface {
	// AddFlags adds a set of flags to the flagset.
	AddFlags(f *pflag.FlagSet)
}

// ControllerFactory allows creation of a Unikorn controller with
// minimal code.
type ControllerFactory interface {
	// Metadata returns the application, version and revision.
	Metadata() util.ServiceDescriptor

	// Options may be nil, otherwise it's a controller specific set of
	// options that are added to the flagset on start up and passed to the
	// reonciler.
	Options() ControllerOptions

	// Reconciler returns a new reconciler instance.
	Reconciler(options *options.Options, controllerOptions ControllerOptions, manager manager.Manager) reconcile.Reconciler

	// RegisterWatches adds any watches that would trigger a reconcile.
	RegisterWatches(manager manager.Manager, controller controller.Controller) error

	// Schemes allows controllers to add types to the client beyond
	// the defaults defined in this repository.
	Schemes() []coreclient.SchemeAdder
}

// ControllerUpgrader optionally allows the factory to define an upgrade procedure.
// DO NOT MODIFY THE SPEC EVER, you have CRD defaulting to fill in any blanks.
// Only things like metadata can be touched, or the resources can be moved around.
// Typically you would want to attach a controller version annotation to define what
// API the resource is using, then drive upgrades based on that.
type ControllerUpgrader interface {
	// Upgrade allows version based upgrades of managed resources.
	Upgrade(ctx context.Context, cli client.Client, options *options.Options) error
}

// ControllerInitializer when implemented on a factory lets it prepare any dependencies,
// after the manager has been constructed and before the factory is called on to create
// a controller.
type ControllerInitializer interface {
	Initialize(ctx context.Context, mgr manager.Manager, opts *options.Options) error
}

// getManager returns a generic manager.
func getManager(f ControllerFactory) (manager.Manager, error) {
	// Create a manager with leadership election to prevent split brain
	// problems, and set the scheme so it gets propagated to the client.
	config, err := clientconfig.GetConfig()
	if err != nil {
		return nil, err
	}

	scheme, err := coreclient.NewScheme(f.Schemes()...)
	if err != nil {
		return nil, err
	}

	service := f.Metadata()

	options := manager.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: service.Name,
	}

	manager, err := manager.New(config, options)
	if err != nil {
		return nil, err
	}

	return manager, nil
}

// getController returns a generic controller.
func getController(o *options.Options, controllerOptions ControllerOptions, manager manager.Manager, f ControllerFactory) (controller.Controller, error) {
	// This prevents a single bad reconcile from affecting all the rest by
	// boning the whole container.
	recoverPanic := true

	options := controller.Options{
		MaxConcurrentReconciles: o.MaxConcurrentReconciles,
		RecoverPanic:            &recoverPanic,
		Reconciler:              f.Reconciler(o, controllerOptions, manager),
	}

	service := f.Metadata()

	c, err := controller.New(service.Name, manager, options)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// doUpgrade allows a controller to optionally define an online upgrade procedure.
func doUpgrade(f ControllerFactory, options *options.Options) error {
	if upgrader, ok := f.(ControllerUpgrader); ok {
		ctx := context.TODO()

		client, err := coreclient.New(ctx, f.Schemes()...)
		if err != nil {
			return err
		}

		if err := upgrader.Upgrade(ctx, client, options); err != nil {
			return err
		}
	}

	return nil
}

func doInitialize(f ControllerFactory, mgr manager.Manager, options *options.Options) error {
	if init, ok := f.(ControllerInitializer); ok {
		ctx := context.TODO()

		if err := init.Initialize(ctx, mgr, options); err != nil {
			return err
		}
	}

	return nil
}

// Run provides common manager initialization and execution.
func Run(f ControllerFactory) {
	o := &options.Options{}
	o.AddFlags(pflag.CommandLine)

	controllerOptions := f.Options()
	if controllerOptions != nil {
		controllerOptions.AddFlags(pflag.CommandLine)
	}

	pflag.Parse()

	o.SetupLogging()

	service := f.Metadata()

	logger := log.Log.WithName("init")
	logger.Info("service starting", "application", service.Name, "version", service.Version, "revision", service.Revision)

	ctx := signals.SetupSignalHandler()

	if err := o.SetupOpenTelemetry(ctx); err != nil {
		logger.Error(err, "open telemetry setup failed")
		os.Exit(1)
	}

	if err := doUpgrade(f, o); err != nil {
		logger.Error(err, "resource upgrade failed")
		os.Exit(1)
	}

	manager, err := getManager(f)
	if err != nil {
		logger.Error(err, "manager creation error")
		os.Exit(1)
	}

	if err := doInitialize(f, manager, o); err != nil {
		logger.Error(err, "factory initialization failed")
		os.Exit(1)
	}

	controller, err := getController(o, controllerOptions, manager, f)
	if err != nil {
		logger.Error(err, "controller creation error")
		os.Exit(1)
	}

	if err := f.RegisterWatches(manager, controller); err != nil {
		logger.Error(err, "watcher registration error")
		os.Exit(1)
	}

	if err := manager.Start(ctx); err != nil {
		logger.Error(err, "manager terminated")
		os.Exit(1)
	}
}
