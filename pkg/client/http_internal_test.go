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

// Internal (package client, not client_test) because these tests need to set
// HTTPClientOptions.spiffeSources directly, or substitute the newSources seam that
// InitSPIFFE dials the Workload API through -- there is no live agent socket in a unit
// test.  Neither is exported, so exercising the mode switch, the SPIFFE TLS wiring and
// the startup hook from outside the package is not possible without this file.
package client

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/spiffetest"
)

const internalTestSPIFFEID = "spiffe://example.org/ns/unikorn-region/sa/region-server"

// TestCredentialSourceSelectsByMode matters because it is the one place that decides
// which credential EncodeAndSign signs with; ApplyTLSClientConfig's SPIFFE branch
// bypasses this method entirely (see spiffe.go), so this is the only test that exercises
// its SPIFFE branch at all.
func TestCredentialSourceSelectsByMode(t *testing.T) {
	t.Parallel()

	t.Run("secret mode returns the secret source", func(t *testing.T) {
		t.Parallel()

		options := &HTTPClientOptions{certificateSource: CertificateSourceSecret}

		source, err := options.credentialSource(nil)
		require.NoError(t, err)

		_, ok := source.(*secretCredentialSource)
		require.True(t, ok, "want *secretCredentialSource, got %T", source)
	})

	t.Run("empty mode defaults to the secret source", func(t *testing.T) {
		t.Parallel()

		options := &HTTPClientOptions{}

		source, err := options.credentialSource(nil)
		require.NoError(t, err)

		_, ok := source.(*secretCredentialSource)
		require.True(t, ok, "want *secretCredentialSource, got %T", source)
	})

	t.Run("spiffe mode returns the SPIFFE source", func(t *testing.T) {
		t.Parallel()

		svid, bundle := spiffetest.NewSVID(t, internalTestSPIFFEID)

		options := &HTTPClientOptions{
			certificateSource: CertificateSourceSPIFFE,
			spiffeServerID:    internalTestSPIFFEID,
			spiffeSources:     &spiffetest.Source{SVID: svid, Bundle: bundle},
		}

		source, err := options.credentialSource(nil)
		require.NoError(t, err)

		_, ok := source.(*spiffeCredentialSource)
		require.True(t, ok, "want *spiffeCredentialSource, got %T", source)
	})
}

// TestCredentialSourceSPIFFEModeRequiresInitialisedSources covers the spiffeSources ==
// nil guard in credentialSource: a caller that reaches this method in SPIFFE mode
// without InitSPIFFE having run must get a clear error, not a nil source or a panic
// later when something calls .Certificate on it.
func TestCredentialSourceSPIFFEModeRequiresInitialisedSources(t *testing.T) {
	t.Parallel()

	options := &HTTPClientOptions{
		certificateSource: CertificateSourceSPIFFE,
		spiffeServerID:    internalTestSPIFFEID,
	}

	_, err := options.credentialSource(nil)
	require.ErrorIs(t, err, ErrClientCredential)
	require.ErrorContains(t, err, "SPIFFE sources not initialised")
}

// TestApplyTLSClientConfigWithoutInitSPIFFEFailsLoudly covers the equivalent guard on
// the handshake path, which is the one credentialSource's own guard above does not
// cover, since ApplyTLSClientConfig's SPIFFE branch never calls credentialSource.
//
// This does NOT say initialisation is optional -- it is mandatory, and every binary
// that selects SPIFFE mode must call InitSPIFFE from its main with the process's root
// context (see TestInitSPIFFEYieldsAUsableCredential for what that must produce).  What
// it pins is that forgetting to do so surfaces as this clear error at startup rather
// than as a nil dereference at the first handshake.
func TestApplyTLSClientConfigWithoutInitSPIFFEFailsLoudly(t *testing.T) {
	t.Parallel()

	options := &HTTPClientOptions{
		certificateSource: CertificateSourceSPIFFE,
		spiffeServerID:    internalTestSPIFFEID,
	}

	err := options.ApplyTLSClientConfig(t.Context(), nil, &tls.Config{MinVersion: tls.VersionTLS13})
	require.ErrorIs(t, err, ErrClientCredential)
	require.ErrorContains(t, err, "SPIFFE sources not initialised")
}

// TestInitSPIFFEYieldsAUsableCredential is the flags-in, credential-out test: it parses
// the flags a SPIFFE-mode deployment actually passes, runs the startup hook a binary's
// main is required to call, and asserts a usable credential comes out.  Every other test
// here starts from an options struct with spiffeSources already populated, which assumes
// away the one step a deployment has to get right.
//
// Both consumers are asserted because they reach the same sources by different routes:
// EncodeAndSign through credentialSource, and the handshake through go-spiffe's hooks,
// which never call credentialSource at all (see spiffe.go).
//
// newSources is substituted rather than dialled, because a unit test has no Workload API
// socket.  The seam is unexported deliberately: no production caller may replace the
// Workload API connection.
func TestInitSPIFFEYieldsAUsableCredential(t *testing.T) {
	t.Parallel()

	svid, bundle := spiffetest.NewSVID(t, internalTestSPIFFEID)

	options := &HTTPClientOptions{
		newSources: func(context.Context) (Sources, io.Closer, error) {
			return &spiffetest.Source{SVID: svid, Bundle: bundle}, noopCloser{}, nil
		},
	}

	flags := pflag.NewFlagSet(t.Name(), pflag.ContinueOnError)
	options.AddFlags(flags)

	require.NoError(t, flags.Parse([]string{
		"--client-certificate-source=spiffe",
		"--spiffe-server-id=" + internalTestSPIFFEID,
	}))

	closer, err := options.InitSPIFFE(t.Context())
	require.NoError(t, err)
	require.NotNil(t, closer)

	t.Cleanup(func() {
		require.NoError(t, closer.Close())
	})

	// The signing path: this is the credential EncodeAndSign signs a principal with, and
	// the SPIFFE ID in the URI SAN is what identity resolves to a system account.
	source, err := options.credentialSource(nil)
	require.NoError(t, err)

	certificate, err := source.Certificate(t.Context())
	require.NoError(t, err)
	require.NotNil(t, certificate.Leaf)
	require.Len(t, certificate.Leaf.URIs, 1)
	require.Equal(t, internalTestSPIFFEID, certificate.Leaf.URIs[0].String())

	// The handshake path: a nil GetClientCertificate here would mean an anonymous
	// client, and a nil VerifyPeerCertificate would mean an unauthenticated server,
	// since go-spiffe's client config verifies the peer there rather than by hostname.
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), nil, config))
	require.NotNil(t, config.VerifyPeerCertificate)
	require.NotNil(t, config.GetClientCertificate)

	presented, err := config.GetClientCertificate(&tls.CertificateRequestInfo{})
	require.NoError(t, err)
	require.Equal(t, [][]byte{svid.Certificates[0].Raw}, presented.Certificate)
}

// TestApplyTLSClientConfigAuthorizesServerBySPIFFEID is the single most
// security-critical behaviour this task adds: that a SPIFFE-mode client actually
// enforces --spiffe-server-id during the handshake, rather than merely trusting any
// certificate that chains to the bundle.  It drives ApplyTLSClientConfig itself (not a
// hand-rolled reimplementation of it) against a real net.Listen/tls.Dial handshake,
// because AuthorizeID's rejection only fires inside VerifyPeerCertificate during an
// actual TLS handshake -- nothing short of one would exercise it.
//
// net.Listen plus tls.NewListener is used instead of httptest.NewTLSServer, which
// injects its own certificate into Certificates/GetCertificate and would fight the
// certificate this test needs the server to present.
func TestApplyTLSClientConfigAuthorizesServerBySPIFFEID(t *testing.T) {
	t.Parallel()

	const (
		serverID   = "spiffe://example.org/ns/unikorn-region/sa/region-server"
		impostorID = "spiffe://example.org/ns/unikorn-region/sa/impostor"
	)

	serverSVID, impostorSVID, bundle := spiffetest.NewSVIDs(t, serverID, impostorID)

	dial := func(t *testing.T, presented *x509svid.SVID) error {
		t.Helper()

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		serverConfig := &tls.Config{
			MinVersion: tls.VersionTLS13,
			Certificates: []tls.Certificate{{
				Certificate: [][]byte{presented.Certificates[0].Raw},
				PrivateKey:  presented.PrivateKey,
			}},
		}

		tlsListener := tls.NewListener(listener, serverConfig)
		defer tlsListener.Close()

		accepted := make(chan error, 1)

		go func() {
			conn, err := tlsListener.Accept()
			if err != nil {
				accepted <- err
				return
			}
			defer conn.Close()

			//nolint:forcetypeassert // tlsListener.Accept always returns a *tls.Conn.
			accepted <- conn.(*tls.Conn).HandshakeContext(t.Context())
		}()

		// Exercises the real production path: the same HTTPClientOptions type and the
		// same ApplyTLSClientConfig method a service calls, not a hand-rolled
		// tlsconfig.HookMTLSClientConfig call.  The client's own presented credential is
		// irrelevant here -- this server does not request or verify one -- so reusing
		// serverSVID keeps the options self-contained.
		clientOptions := &HTTPClientOptions{
			certificateSource: CertificateSourceSPIFFE,
			spiffeServerID:    serverID,
			spiffeSources:     &spiffetest.Source{SVID: serverSVID, Bundle: bundle},
		}

		config := &tls.Config{MinVersion: tls.VersionTLS13}
		require.NoError(t, clientOptions.ApplyTLSClientConfig(t.Context(), nil, config))

		conn, dialErr := tls.Dial("tcp", listener.Addr().String(), config)
		if dialErr == nil {
			conn.Close()
		}

		// Drain the server goroutine so it does not leak past the test; its own view of
		// the handshake is not asserted on, since this test is about the client's
		// authorization decision.
		<-accepted

		return dialErr
	}

	t.Run("server presenting the configured ID completes the handshake", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, dial(t, serverSVID))
	})

	t.Run("server presenting a different ID fails the handshake", func(t *testing.T) {
		t.Parallel()

		// Asserts on AuthorizeID's own rejection message (spiffeid.MatchID:
		// `unexpected ID %q`), confirmed by inspection of what tls.Dial actually
		// returns here, so this fails for ID mismatch specifically -- not for some
		// other reason, such as a broken bundle, that would also produce a dial error.
		err := dial(t, impostorSVID)
		require.ErrorContains(t, err, "unexpected ID")
		require.ErrorContains(t, err, impostorID)
	})
}
