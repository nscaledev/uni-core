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

package options

import (
	"runtime"

	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/cd"
)

// BaseOptions are things all controllers, message consumers and servers will need.
type BaseOptions struct {
	// Namespace is the namespace we are running in.
	Namespace string
}

func (o *BaseOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.Namespace, "namespace", "", "Namespace the process is running in")
}

// Options defines common controller options.
type Options struct {
	BaseOptions

	// MaxConcurrentReconciles allows requests to be processed
	// concurrently.  Be warned, this will inrcrease memory utilization
	// and may need to update the Helm limits.
	MaxConcurrentReconciles int

	// CDDriver defines the continuous-delivery backend driver to use
	// to manage applications.
	CDDriver cd.DriverKindFlag
}

func (o *Options) AddFlags(flags *pflag.FlagSet) {
	o.CDDriver.Kind = cd.DriverKindArgoCD

	o.BaseOptions.AddFlags(flags)

	flags.IntVar(&o.MaxConcurrentReconciles, "max-concurrency", runtime.NumCPU(), "Maximum number of requests to process at the same time")
	flags.Var(&o.CDDriver, "cd-driver", "CD backend driver to use from [argocd]")
}
