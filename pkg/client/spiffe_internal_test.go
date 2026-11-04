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

package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"
)

const (
	identityID      = "spiffe://example.org/identity"
	regionID        = "spiffe://example.org/region"
	identityHost    = "https://identity.example.com"
	identityAddress = "https://identity.identity.svc:6082"
)

// spiffeTestPKI is a CA for the trust domain example.org.
type spiffeTestPKI struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	bundle      *x509bundle.Bundle
}

func newSPIFFETestPKI(t *testing.T) *spiffeTestPKI {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certificate, err := x509.ParseCertificate(certificateDER)
	require.NoError(t, err)

	return &spiffeTestPKI{
		certificate: certificate,
		key:         key,
		bundle:      x509bundle.FromX509Authorities(spiffeid.RequireTrustDomainFromString("example.org"), []*x509.Certificate{certificate}),
	}
}

// svid issues a new X.509-SVID for id, with a random serial number.
func (p *spiffeTestPKI) svid(t *testing.T, id string) *x509svid.SVID {
	t.Helper()

	spiffeID := spiffeid.RequireFromString(id)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		URIs:                  []*url.URL{spiffeID.URL()},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, p.certificate, &key.PublicKey, p.key)
	require.NoError(t, err)

	certificate, err := x509.ParseCertificate(certificateDER)
	require.NoError(t, err)

	return &x509svid.SVID{
		ID:           spiffeID,
		Certificates: []*x509.Certificate{certificate},
		PrivateKey:   key,
	}
}

// staticSource is a SPIFFE source with a fixed trust bundle.  A test can change
// its SVID.
type staticSource struct {
	svid   atomic.Pointer[x509svid.SVID]
	bundle *x509bundle.Bundle
}

func newStaticSource(svid *x509svid.SVID, bundle *x509bundle.Bundle) *staticSource {
	s := &staticSource{bundle: bundle}
	s.svid.Store(svid)

	return s
}

func (s *staticSource) GetX509SVID() (*x509svid.SVID, error) {
	return s.svid.Load(), nil
}

func (s *staticSource) GetX509BundleForTrustDomain(trustDomain spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return s.bundle.GetX509BundleForTrustDomain(trustDomain)
}

func (s *staticSource) Close() error {
	return nil
}

func parseFlags(t *testing.T, options *HTTPOptions, clientOptions *HTTPClientOptions, args ...string) {
	t.Helper()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	if options != nil {
		options.AddFlags(flags)
	}

	clientOptions.AddFlags(flags)

	require.NoError(t, flags.Parse(args))
}

// spiffeOptions returns the options of an identity client in SPIFFE mode, as
// region sets them, with source as the open SVID source.  Each argument in args
// overrides a flag of that configuration.
func spiffeOptions(t *testing.T, source spiffeSource, args ...string) (*HTTPOptions, *HTTPClientOptions) {
	t.Helper()

	options := NewHTTPOptions("identity")
	clientOptions := &HTTPClientOptions{}

	parseFlags(t, options, clientOptions, append([]string{
		"--client-certificate-source=spiffe",
		"--identity-host=" + identityHost,
		"--identity-spiffe-address=" + identityAddress,
		"--identity-spiffe-server-id=" + identityID,
	}, args...)...)

	clientOptions.spiffe = source

	return options, clientOptions
}

// serveSPIFFE serves a test server that presents the SVID of serverID and
// accepts any client SVID of the trust domain.  It sends the leaf certificate of
// the client of each request.
func serveSPIFFE(t *testing.T, pki *spiffeTestPKI, serverID string) (string, <-chan *x509.Certificate) {
	t.Helper()

	peers := make(chan *x509.Certificate, 10)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		peers <- r.TLS.PeerCertificates[0]
	}))

	server.Listener = tls.NewListener(server.Listener, tlsconfig.MTLSServerConfig(pki.svid(t, serverID), pki.bundle, tlsconfig.AuthorizeAny()))
	server.Start()

	t.Cleanup(server.Close)

	return "https://" + server.Listener.Addr().String(), peers
}

// get sends one request on a new connection and expects a 200.
func get(t *testing.T, config *tls.Config, address string) error {
	t.Helper()

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: config, DisableKeepAlives: true},
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	require.Equal(t, http.StatusOK, response.StatusCode)

	return nil
}

// TestSetupSPIFFERefusesASecretCertificate checks that a process with a Secret
// certificate and the SPIFFE source does not start.  The error names both flags
// with their prefix, so that the operator can find the fault.
func TestSetupSPIFFERefusesASecretCertificate(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"--identity-client-certificate-name=client-cert", "--identity-client-certificate-namespace=test"} {
		clientOptions := NewHTTPClientOptions("identity")

		parseFlags(t, nil, clientOptions, "--identity-client-certificate-source=spiffe", "--identity-client-spiffe-start-timeout=100ms", arg)

		err := clientOptions.SetupSPIFFE(t.Context())
		require.ErrorIs(t, err, errSPIFFEConfiguration, arg)
		require.ErrorContains(t, err, "--identity-client-certificate-name", arg)
		require.ErrorContains(t, err, "--identity-client-certificate-namespace", arg)
	}
}

func TestUnknownCertificateSourceIsRefused(t *testing.T) {
	t.Parallel()

	clientOptions := &HTTPClientOptions{}

	parseFlags(t, nil, clientOptions, "--client-certificate-source=file")

	require.ErrorIs(t, clientOptions.SetupSPIFFE(t.Context()), errSPIFFEConfiguration)

	_, err := TLSClientConfig(t.Context(), nil, NewHTTPOptions("identity"), clientOptions)
	require.ErrorIs(t, err, errSPIFFEConfiguration)
}

// TestSetupSPIFFEDoesNothingInSecretMode checks the default.  Every service that
// does not use SPIFFE calls SetupSPIFFE, and it must start as before.
func TestSetupSPIFFEDoesNothingInSecretMode(t *testing.T) {
	t.Parallel()

	clientOptions := &HTTPClientOptions{}

	parseFlags(t, nil, clientOptions)

	require.NoError(t, clientOptions.SetupSPIFFE(t.Context()))
	require.Nil(t, clientOptions.spiffe)
	require.NoError(t, clientOptions.Close())
}

// TestSPIFFETLSClientConfigRefusesAnIncompleteConfiguration checks each fault
// that stops a client in SPIFFE mode.  The base configuration passes, so each
// case fails only because of its own change.
func TestSPIFFETLSClientConfigRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	pki := newSPIFFETestPKI(t)
	source := newStaticSource(pki.svid(t, regionID), pki.bundle)

	options, clientOptions := spiffeOptions(t, source)

	config, err := TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS13), config.MinVersion)

	tests := []struct {
		name   string
		source spiffeSource
		args   []string
	}{
		{name: "source not open", source: nil},
		{name: "CA secret name", source: source, args: []string{"--identity-ca-secret-name=ca"}},
		{name: "CA secret namespace", source: source, args: []string{"--identity-ca-secret-namespace=test"}},
		{name: "empty server ID", source: source, args: []string{"--identity-spiffe-server-id="}},
		{name: "malformed server ID", source: source, args: []string{"--identity-spiffe-server-id=identity"}},
		{name: "server ID in another trust domain", source: source, args: []string{"--identity-spiffe-server-id=spiffe://other.org/identity"}},
		{name: "no SPIFFE address", source: source, args: []string{"--identity-spiffe-address="}},
		{name: "http SPIFFE address", source: source, args: []string{"--identity-spiffe-address=http://identity.identity.svc:6082"}},
		{name: "SPIFFE address with no scheme", source: source, args: []string{"--identity-spiffe-address=identity.identity.svc:6082"}},
		{name: "https SPIFFE address with no host", source: source, args: []string{"--identity-spiffe-address=https:///"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options, clientOptions := spiffeOptions(t, test.source, test.args...)

			_, err := TLSClientConfig(t.Context(), nil, options, clientOptions)
			require.ErrorIs(t, err, errSPIFFEConfiguration)
		})
	}
}

// TestSecretModeRefusesASPIFFEAddress checks that a Secret mode client never
// connects to the SPIFFE address that Host returns.
func TestSecretModeRefusesASPIFFEAddress(t *testing.T) {
	t.Parallel()

	options := NewHTTPOptions("identity")
	clientOptions := &HTTPClientOptions{}

	parseFlags(t, options, clientOptions, "--identity-host="+identityHost)

	_, err := TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)

	options = NewHTTPOptions("identity")
	clientOptions = &HTTPClientOptions{}

	parseFlags(t, options, clientOptions, "--identity-host="+identityHost, "--identity-spiffe-address="+identityAddress)

	_, err = TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.ErrorIs(t, err, errSPIFFEConfiguration)
}

func TestSPIFFEClientAcceptsOnlyTheServerID(t *testing.T) {
	t.Parallel()

	pki := newSPIFFETestPKI(t)
	address, _ := serveSPIFFE(t, pki, identityID)
	source := newStaticSource(pki.svid(t, regionID), pki.bundle)

	options, clientOptions := spiffeOptions(t, source)

	config, err := TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)
	require.NoError(t, get(t, config, address))

	options, clientOptions = spiffeOptions(t, source, "--identity-spiffe-server-id=spiffe://example.org/other")

	config, err = TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)
	require.ErrorContains(t, get(t, config, address), `unexpected ID "`+identityID+`"`)
}

// TestSPIFFEClientPresentsTheCurrentSVID checks that one TLS configuration
// presents a renewed SVID on the next connection, with no restart.
func TestSPIFFEClientPresentsTheCurrentSVID(t *testing.T) {
	t.Parallel()

	pki := newSPIFFETestPKI(t)
	address, peers := serveSPIFFE(t, pki, identityID)

	first := pki.svid(t, regionID)
	second := pki.svid(t, regionID)
	source := newStaticSource(first, pki.bundle)

	options, clientOptions := spiffeOptions(t, source)

	config, err := TLSClientConfig(t.Context(), nil, options, clientOptions)
	require.NoError(t, err)

	require.NoError(t, get(t, config, address))
	require.Equal(t, first.Certificates[0].Raw, (<-peers).Raw)

	source.svid.Store(second)

	require.NoError(t, get(t, config, address))
	require.Equal(t, second.Certificates[0].Raw, (<-peers).Raw)
}

// TestSPIFFEFlagsTakeThePrefix checks the flag names that the charts emit, and
// their defaults.  A process can hold a prefixed and an unprefixed client.
func TestSPIFFEFlagsTakeThePrefix(t *testing.T) {
	t.Parallel()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	NewHTTPOptions("identity").AddFlags(flags)

	require.NotPanics(t, func() {
		NewHTTPClientOptions("identity").AddFlags(flags)
		(&HTTPClientOptions{}).AddFlags(flags)
	})

	for _, name := range []string{"identity-spiffe-server-id", "identity-spiffe-address"} {
		require.NotNil(t, flags.Lookup(name), name)
	}

	for _, prefix := range []string{"identity-", ""} {
		source := flags.Lookup(prefix + "client-certificate-source")
		require.NotNil(t, source, prefix)
		require.Equal(t, "secret", source.DefValue)

		timeout := flags.Lookup(prefix + "client-spiffe-start-timeout")
		require.NotNil(t, timeout, prefix)
		require.Equal(t, "30s", timeout.DefValue)
	}

	for _, name := range []string{"spiffe-server-id", "spiffe-address", "-spiffe-server-id", "-spiffe-address", "-client-certificate-source", "-client-spiffe-start-timeout"} {
		require.Nil(t, flags.Lookup(name), name)
	}
}

// TestHostPrefersTheSPIFFEAddress checks the two endpoint URLs.  A client
// connects to Host.  A service publishes PublicHost, which must not change when
// the client moves to the SPIFFE address.
func TestHostPrefersTheSPIFFEAddress(t *testing.T) {
	t.Parallel()

	options := NewHTTPOptions("identity")

	parseFlags(t, options, &HTTPClientOptions{}, "--identity-host="+identityHost)

	require.Equal(t, identityHost, options.Host())
	require.Equal(t, identityHost, options.PublicHost())

	options = NewHTTPOptions("identity")

	parseFlags(t, options, &HTTPClientOptions{}, "--identity-host="+identityHost, "--identity-spiffe-address="+identityAddress)

	require.Equal(t, identityAddress, options.Host())
	require.Equal(t, identityHost, options.PublicHost())
}

// TestSetupSPIFFEWaitIsBounded checks that the process does not wait forever
// for a Workload API that is not there.  It sets an environment variable, so it
// does not run in parallel.
func TestSetupSPIFFEWaitIsBounded(t *testing.T) {
	t.Setenv("SPIFFE_ENDPOINT_SOCKET", "unix://"+t.TempDir()+"/missing.sock")

	clientOptions := &HTTPClientOptions{}

	parseFlags(t, nil, clientOptions, "--client-certificate-source=spiffe", "--client-spiffe-start-timeout=100ms")

	start := time.Now()

	err := clientOptions.SetupSPIFFE(t.Context())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "the start timeout bounds the wait")
	require.NoError(t, clientOptions.Close())
}
