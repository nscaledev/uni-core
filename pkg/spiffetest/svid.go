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

// Package spiffetest mints X509-SVID shaped credentials for tests, so that tests
// exercise the certificate shape SPIRE actually issues rather than a common-name
// certificate that happens to be signed.
package spiffetest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

// NewSVID mints a CA and a leaf X509-SVID beneath it, returning the SVID and a
// bundle containing the CA.
//
// The leaf mirrors what SPIRE issues: subject C=US O=SPIRE with no common name, the
// SPIFFE ID in a URI SAN, an EC P-256 key, client and server extended key usage, and
// IsCA false.  The last is load-bearing: x509svid.Verify rejects a leaf that is a CA,
// so a self-signed single certificate cannot stand in for an SVID here.
func NewSVID(t *testing.T, id string) (*x509svid.SVID, *x509bundle.Bundle) {
	t.Helper()

	ca, caKey := newTestCA(t)
	svid := newLeafSVID(t, id, 2, ca, caKey)

	return svid, x509bundle.FromX509Authorities(svid.ID.TrustDomain(), []*x509.Certificate{ca})
}

// NewSVIDs mints two leaf X509-SVIDs, with the given IDs, under one shared CA, and
// returns both alongside the single bundle that verifies either.  Tests that need two
// peers to trust each other's certificate -- for example, a client and an impostor
// server in a handshake test -- use this instead of two independent NewSVID calls,
// which would mint two unrelated CAs that do not trust one another.
func NewSVIDs(t *testing.T, idA, idB string) (*x509svid.SVID, *x509svid.SVID, *x509bundle.Bundle) {
	t.Helper()

	ca, caKey := newTestCA(t)

	svidA := newLeafSVID(t, idA, 2, ca, caKey)
	svidB := newLeafSVID(t, idB, 3, ca, caKey)

	return svidA, svidB, x509bundle.FromX509Authorities(svidA.ID.TrustDomain(), []*x509.Certificate{ca})
}

// newTestCA mints a self-signed EC P-256 CA certificate for signing leaf SVIDs.
func newTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "spiffetest"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}

	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parsing CA certificate: %v", err)
	}

	return ca, caKey
}

// newLeafSVID mints a leaf X509-SVID for id, signed by ca/caKey.  serial must be unique
// among certificates signed by the same ca.
func newLeafSVID(t *testing.T, id string, serial int64, ca *x509.Certificate, caKey *ecdsa.PrivateKey) *x509svid.SVID {
	t.Helper()

	spiffeID, err := spiffeid.FromString(id)
	if err != nil {
		t.Fatalf("parsing SPIFFE ID %q: %v", id, err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating leaf key: %v", err)
	}

	uri, err := url.Parse(spiffeID.String())
	if err != nil {
		t.Fatalf("parsing SPIFFE ID as a URI: %v", err)
	}

	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{Country: []string{"US"}, Organization: []string{"SPIRE"}},
		URIs:                  []*url.URL{uri},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:                  false,
		BasicConstraintsValid: true,
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating leaf certificate: %v", err)
	}

	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatalf("parsing leaf certificate: %v", err)
	}

	return &x509svid.SVID{
		ID:           spiffeID,
		Certificates: []*x509.Certificate{leaf},
		PrivateKey:   leafKey,
	}
}

// Source is a static x509svid.Source and x509bundle.Source pair, standing in for
// workloadapi.X509Source.
type Source struct {
	SVID   *x509svid.SVID
	Bundle *x509bundle.Bundle
}

func (s *Source) GetX509SVID() (*x509svid.SVID, error) {
	return s.SVID, nil
}

func (s *Source) GetX509BundleForTrustDomain(spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return s.Bundle, nil
}
