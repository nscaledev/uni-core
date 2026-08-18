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
	goerrors "errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrClientCredential is returned when the client credential cannot be sourced.
var ErrClientCredential = goerrors.New("client credential")

// credentialSource yields the service's current client credential.  EncodeAndSign
// reads it here to sign a principal with the private key, and in Secret mode the TLS
// handshake reads it here too, to present the certificate; a tls.Certificate carries
// both, so one method serves both.  In SPIFFE mode the handshake does NOT come through
// here -- go-spiffe is handed the same Sources directly, so the two cannot disagree
// about the credential even though only one of them is this interface.  See
// spiffe.go:36-40.
type credentialSource interface {
	Certificate(ctx context.Context) (*tls.Certificate, error)
}

// secretCredentialSource reads the credential from a Kubernetes Secret, which is
// how every UNI service sourced it before SPIFFE.
type secretCredentialSource struct {
	options *HTTPClientOptions
	client  client.Client
}

func (s *secretCredentialSource) Certificate(ctx context.Context) (*tls.Certificate, error) {
	return s.options.loadTLSCertificate(ctx, s.client)
}
