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

package options_test

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"

	"github.com/unikorn-cloud/core/pkg/manager/options"
)

// TestRequeuePeriodDefault pins the flag default.  The period is only read by a
// controller that has opted into polling, so a non-zero default is safe, but it
// must stay a sane one: it becomes the re-observation cadence for every polling
// controller that does not override it.
func TestRequeuePeriodDefault(t *testing.T) {
	t.Parallel()

	o := &options.Options{}
	o.AddFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))

	assert.Equal(t, time.Minute, o.RequeuePeriod)
}

// TestRequeuePeriodOverride checks the flag actually binds.
func TestRequeuePeriodOverride(t *testing.T) {
	t.Parallel()

	o := &options.Options{}

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.AddFlags(flags)

	assert.NoError(t, flags.Parse([]string{"--requeue-period=5m"}))
	assert.Equal(t, 5*time.Minute, o.RequeuePeriod)
}
