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
	"crypto/ecdsa"
	"crypto/x509"
	"testing"

	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/require"

	"github.com/unikorn-cloud/core/pkg/client"
	"github.com/unikorn-cloud/core/pkg/spiffetest"
)

const testSPIFFEID = "spiffe://example.org/ns/unikorn-region/sa/region-server"

// TestSPIFFECredentialCarriesTheSVID matters because the certificate the handshake
// presents is what uni-identity resolves to a system account.  If the leaf were the
// CA, or the SAN were dropped, identity would resolve to the wrong subject or to
// nothing, and the failure would surface as an unregistered system account rather
// than as a credential fault.
func TestSPIFFECredentialCarriesTheSVID(t *testing.T) {
	t.Parallel()

	svid, bundle := spiffetest.NewSVID(t, testSPIFFEID)

	certificate, err := client.SPIFFECredential(&spiffetest.Source{SVID: svid, Bundle: bundle}).Certificate(t.Context())
	if err != nil {
		t.Fatalf("sourcing certificate: %v", err)
	}

	if got := len(certificate.Certificate); got != 1 {
		t.Fatalf("chain length: got %d, want 1", got)
	}

	if certificate.Leaf == nil {
		t.Fatal("leaf not populated: EncodeAndSign reads it to select a signature algorithm")
	}

	if got := certificate.Leaf.Subject.CommonName; got != "" {
		t.Errorf("common name: got %q, want empty, which is what makes the URI SAN fallback fire", got)
	}

	if got := len(certificate.Leaf.URIs); got != 1 || certificate.Leaf.URIs[0].String() != testSPIFFEID {
		t.Errorf("URI SAN: got %v, want [%s]", certificate.Leaf.URIs, testSPIFFEID)
	}
}

// TestSPIFFECredentialCarriesAnECKey matters because EncodeAndSign selects its
// signature algorithm from the key type.  An SVID is EC P-256, and signing required
// RSA until finding 11; this asserts the key reaching that code is still EC, so a
// regression there fails here rather than in a controller at runtime.
func TestSPIFFECredentialCarriesAnECKey(t *testing.T) {
	t.Parallel()

	svid, bundle := spiffetest.NewSVID(t, testSPIFFEID)

	certificate, err := client.SPIFFECredential(&spiffetest.Source{SVID: svid, Bundle: bundle}).Certificate(t.Context())
	if err != nil {
		t.Fatalf("sourcing certificate: %v", err)
	}

	if _, ok := certificate.PrivateKey.(*ecdsa.PrivateKey); !ok {
		t.Fatalf("private key type: got %T, want *ecdsa.PrivateKey", certificate.PrivateKey)
	}
}

// TestSPIFFECredentialPreservesChainOrder exercises SPIFFECredential's own mapping,
// rather than a property of the spiffetest fixture the way the two tests above do.  A
// real SVID from an identity provider behind an intermediate carries more than one
// certificate; if the mapping ever reordered or truncated the chain, a client would
// present the wrong intermediate (or none) to a peer, and the failure would surface as
// a verification error far from this code rather than here.
func TestSPIFFECredentialPreservesChainOrder(t *testing.T) {
	t.Parallel()

	leaf, bundle := spiffetest.NewSVID(t, testSPIFFEID)
	other, _ := spiffetest.NewSVID(t, testSPIFFEID)

	// The second certificate stands in for an intermediate.  SPIFFECredential only maps
	// raw bytes; it does not validate the chain (that is x509svid.Verify's job), so any
	// second, distinguishable certificate proves the mapping without needing a
	// cryptographically valid intermediate.
	svid := &x509svid.SVID{
		ID:           leaf.ID,
		Certificates: []*x509.Certificate{leaf.Certificates[0], other.Certificates[0]},
		PrivateKey:   leaf.PrivateKey,
	}

	certificate, err := client.SPIFFECredential(&spiffetest.Source{SVID: svid, Bundle: bundle}).Certificate(t.Context())
	require.NoError(t, err)

	require.Len(t, certificate.Certificate, 2)
	require.Equal(t, leaf.Certificates[0].Raw, certificate.Certificate[0])
	require.Equal(t, other.Certificates[0].Raw, certificate.Certificate[1])
	require.Same(t, leaf.Certificates[0], certificate.Leaf)
}

// TestInitSPIFFERejectsInvalidConfiguration matters because ApplyTLSClientConfig's
// SPIFFE branch never calls credentialSource (see spiffe.go), so credentialSource's own
// conflict check only runs for a client that calls EncodeAndSign.  InitSPIFFE is the
// startup hook that must catch these regardless of whether the process ever signs a
// principal, so a misconfigured service fails at startup rather than by silently
// presenting an SVID while ignoring stray Secret flags, or by trusting any peer in our
// trust domain because no server ID was configured.
func TestInitSPIFFERejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unrecognised certificate source",
			args: []string{"--client-certificate-source=bogus"},
			want: "unknown --client-certificate-source",
		},
		{
			name: "spiffe mode conflicts with a client certificate secret",
			args: []string{
				"--client-certificate-source=spiffe",
				"--spiffe-server-id=" + testSPIFFEID,
				"--client-certificate-name=client-cert",
				"--client-certificate-namespace=test",
			},
			want: "conflicts with --client-certificate-name",
		},
		{
			name: "spiffe mode without a server ID",
			args: []string{"--client-certificate-source=spiffe"},
			want: "--spiffe-server-id is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			options := &client.HTTPClientOptions{}
			flags := pflagSet(t, options)
			require.NoError(t, flags.Parse(tc.args))

			closer, err := options.InitSPIFFE(t.Context())
			require.Nil(t, closer)
			require.ErrorIs(t, err, client.ErrClientCredential)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
