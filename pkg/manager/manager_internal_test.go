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

package manager

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/unikorn-cloud/core/pkg/manager/options"

	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const freshProcessEnv = "MANAGER_TEST_FRESH_PROCESS"

// inFreshProcess reruns the calling test alone in a new test process, because
// metrics setup is once per process. It returns true in that process, where
// the test body should run.
func inFreshProcess(t *testing.T) bool {
	t.Helper()

	if os.Getenv(freshProcessEnv) == t.Name() {
		return true
	}

	//nolint:gosec
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
	cmd.Env = append(os.Environ(), freshProcessEnv+"="+t.Name())

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "--- PASS: "+t.Name())

	return false
}

// scraped reports whether an OTel instrument created after setup is exported
// on the controller-runtime registry.
func scraped(t *testing.T) bool {
	t.Helper()

	counter, err := otel.Meter("manager-test").Int64Counter("test_setup_telemetry")
	require.NoError(t, err)
	counter.Add(t.Context(), 1)

	families, err := ctrlmetrics.Registry.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == "test_setup_telemetry_total" {
			return true
		}
	}

	return false
}

func TestScrapeTelemetryScrapes(t *testing.T) {
	t.Parallel()

	if !inFreshProcess(t) {
		return
	}

	o := &options.Options{}
	o.TraceSampingRatio = 1

	require.NoError(t, scrapeTelemetry("unikorn-test")(t.Context(), o))
	assert.True(t, scraped(t))

	// Traces are named by the service too.
	_, span := otel.Tracer("manager-test").Start(t.Context(), "test")
	span.End()

	recorded, ok := span.(sdktrace.ReadOnlySpan)
	require.True(t, ok)

	name, ok := recorded.Resource().Set().Value("service.name")
	require.True(t, ok)
	assert.Equal(t, "unikorn-test", name.AsString())
}

func TestPushTelemetryDoesNotScrape(t *testing.T) {
	t.Parallel()

	if !inFreshProcess(t) {
		return
	}

	require.NoError(t, pushTelemetry(t.Context(), &options.Options{}))
	assert.False(t, scraped(t))
}
