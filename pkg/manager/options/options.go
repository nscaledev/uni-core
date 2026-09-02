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

package options

import (
	"runtime"
	"time"

	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/cd"
	"github.com/unikorn-cloud/core/pkg/constants"
	"github.com/unikorn-cloud/core/pkg/options"
)

// Options defines common controller options.
type Options struct {
	options.CoreOptions

	// MaxConcurrentReconciles allows requests to be processed
	// concurrently.  Be warned, this will inrcrease memory utilization
	// and may need to update the Helm limits.
	MaxConcurrentReconciles int

	// RequeuePeriod is how long a polling controller waits before revisiting a
	// resource it has no further work for.  It is read only when the reconciler
	// was created with manager.WithPolling, so the default is inert for every
	// controller that does not opt in.
	//
	// A non-positive value is not a supported way to turn polling off - the
	// controller polls because of what backs its resources, which is not an
	// operator decision - so a polling reconciler falls back to
	// constants.DefaultRequeuePeriod and logs that it has done so.
	RequeuePeriod time.Duration

	// CDDriver defines the continuous-delivery backend driver to use
	// to manage applications.
	CDDriver cd.DriverKindFlag
}

func (o *Options) AddFlags(flags *pflag.FlagSet) {
	o.CDDriver.Kind = cd.DriverKindArgoCD

	o.CoreOptions.AddFlags(flags)

	flags.IntVar(&o.MaxConcurrentReconciles, "max-concurrency", runtime.NumCPU(), "Maximum number of requests to process at the same time")
	flags.DurationVar(&o.RequeuePeriod, "requeue-period", constants.DefaultRequeuePeriod, "Period after which a polling controller revisits a resource that needs no further work, ignored by controllers that do not poll")
	flags.Var(&o.CDDriver, "cd-driver", "CD backend driver to use from [argocd]")
}
