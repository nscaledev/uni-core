/*
Copyright 2022-2025 EscherCloud.
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

package manager

import (
	"context"
	"flag"
	"os"

	"github.com/spf13/pflag"

	coreclient "github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/manager/options"
	"github.com/unikorn-cloud/core/pkg/manager/otel"

	klog "k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	Metadata() (string, string, string)

	// Options may be nil, otherwise it's a controller specific set of
	// options that are added to the flagset on start up and passed to the
	// reonciler.
	Options() ControllerOptions

	// Reconciler returns a new reconciler instance.
	Reconciler(options *options.Options, controllerOptions ControllerOptions, manager manager.Manager) reconcile.Reconciler

	// RegisterWatches adds any watches that would trigger a reconcile.
	RegisterWatches(manager manager.Manager, controller controller.Controller) error

	// Upgrade allows version based upgrades of managed resources.
	// DO NOT MODIFY THE SPEC EVER.  Only things like metadata can
	// be touched.
	Upgrade(client client.Client) error

	// Schemes allows controllers to add types to the client beyond
	// the defaults defined in this repository.
	Schemes() []coreclient.SchemeAdder
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

	application, _, _ := f.Metadata()

	options := manager.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: application,
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

	application, _, _ := f.Metadata()

	c, err := controller.New(application, manager, options)
	if err != nil {
		return nil, err
	}

	return c, nil
}

func doUpgrade(f ControllerFactory) error {
	client, err := coreclient.New(context.TODO())
	if err != nil {
		return err
	}

	if err := f.Upgrade(client); err != nil {
		return err
	}

	return nil
}

// Run provides common manager initialization and execution.
func Run(f ControllerFactory) {
	zapOptions := &zap.Options{}
	zapOptions.BindFlags(flag.CommandLine)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	o := &options.Options{}
	o.AddFlags(pflag.CommandLine)

	otelOptions := &otel.Options{}
	otelOptions.AddFlags(pflag.CommandLine)

	controllerOptions := f.Options()
	if controllerOptions != nil {
		controllerOptions.AddFlags(pflag.CommandLine)
	}

	pflag.Parse()

	logr := zap.New(zap.UseFlagOptions(zapOptions))

	log.SetLogger(logr)
	klog.SetLogger(logr)

	application, version, revision := f.Metadata()

	logger := log.Log.WithName("init")
	logger.Info("service starting", "application", application, "version", version, "revision", revision)

	ctx := signals.SetupSignalHandler()

	if err := otelOptions.Setup(ctx); err != nil {
		logger.Error(err, "open telemetry setup failed")
		os.Exit(1)
	}

	if err := doUpgrade(f); err != nil {
		logger.Error(err, "resource upgrade failed")
		os.Exit(1)
	}

	manager, err := getManager(f)
	if err != nil {
		logger.Error(err, "manager creation error")
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
