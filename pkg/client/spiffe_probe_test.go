//go:build spiffeprobe

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

package client_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	coreclient "github.com/unikorn-cloud/core/pkg/client"
)

// TestSPIFFEProbe calls a SPIFFE endpoint as a service in SPIFFE mode does.  It
// is a manual check, so it needs the build tag spiffeprobe, the SPIFFE Workload
// API, PROBE_URL and PROBE_SERVER_ID.
func TestSPIFFEProbe(t *testing.T) {
	t.Parallel()

	url := os.Getenv("PROBE_URL")
	serverID := os.Getenv("PROBE_SERVER_ID")

	require.NotEmpty(t, url, "PROBE_URL must be set")
	require.NotEmpty(t, serverID, "PROBE_SERVER_ID must be set")

	options := coreclient.NewHTTPOptions("identity")
	clientOptions := &coreclient.HTTPClientOptions{}

	flags := pflag.NewFlagSet("probe", pflag.ContinueOnError)
	options.AddFlags(flags)
	clientOptions.AddFlags(flags)

	require.NoError(t, flags.Parse([]string{
		"--client-certificate-source=spiffe",
		"--identity-spiffe-address=" + url,
		"--identity-spiffe-server-id=" + serverID,
	}))

	require.NoError(t, clientOptions.SetupSPIFFE(t.Context()))

	t.Cleanup(func() {
		require.NoError(t, clientOptions.Close())
	})

	config, err := coreclient.TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: config},
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, options.Host(), nil)
	require.NoError(t, err)

	// A system account call needs a principal.  The probe sends it unsigned.
	request.Header.Set("X-Principal", base64.RawURLEncoding.EncodeToString([]byte(`{"actor":"spiffe-probe"}`)))

	response, err := client.Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	t.Logf("status: %d", response.StatusCode)
	t.Logf("body: %s", body)
}
