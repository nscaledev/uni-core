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
	"crypto/tls"
	"fmt"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

// Sources is the pair go-spiffe needs: our own SVID, and the bundle peers are
// verified against.  workloadapi.X509Source implements both; tests substitute a
// static pair.
type Sources interface {
	x509svid.Source
	x509bundle.Source
}

// spiffeCredentialSource adapts an X509-SVID to credentialSource, so the signed
// principal reads it through EncodeAndSign.  The TLS handshake does not go through
// this type: ApplyTLSClientConfig hands go-spiffe's own tlsconfig.HookMTLSClientConfig
// the Sources directly.  Both read the same Sources, so they cannot disagree about the
// credential even though only one of them is this type.
type spiffeCredentialSource struct {
	sources Sources
}

// SPIFFECredential returns a credential sourced from the SPIFFE Workload API.
// Exported for tests; production callers reach it through credentialSource.
func SPIFFECredential(sources Sources) credentialSource {
	return &spiffeCredentialSource{sources: sources}
}

func (s *spiffeCredentialSource) Certificate(_ context.Context) (*tls.Certificate, error) {
	svid, err := s.sources.GetX509SVID()
	if err != nil {
		return nil, fmt.Errorf("%w: fetching SVID: %w", ErrClientCredential, err)
	}

	if len(svid.Certificates) == 0 {
		return nil, fmt.Errorf("%w: SVID carries no certificates", ErrClientCredential)
	}

	chain := make([][]byte, 0, len(svid.Certificates))

	for _, certificate := range svid.Certificates {
		chain = append(chain, certificate.Raw)
	}

	// Leaf is populated deliberately: EncodeAndSign reads the public key from it to
	// select a signature algorithm, and leaving it nil makes that a parse per call.
	return &tls.Certificate{
		Certificate: chain,
		PrivateKey:  svid.PrivateKey,
		Leaf:        svid.Certificates[0],
	}, nil
}
