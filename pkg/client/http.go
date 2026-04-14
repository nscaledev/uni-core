/*
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

package client

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/errors"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultClientCertificateReloadInterval = 24 * time.Hour

// HTTPOptions are generic options for HTTP clients.
type HTTPOptions struct {
	// service determines the CLI flag prefix.
	service string
	// host is the identity Host name.
	host string
	// secretNamespace tells us where to source the CA secret.
	secretNamespace string
	// secretName is the root CA secret of the identity endpoint.
	secretName string
}

func NewHTTPOptions(service string) *HTTPOptions {
	return &HTTPOptions{
		service: service,
	}
}

func (o *HTTPOptions) Host() string {
	return o.host
}

// AddFlags adds the options to the CLI flags.
func (o *HTTPOptions) AddFlags(f *pflag.FlagSet) {
	f.StringVar(&o.host, o.service+"-host", "", "Identity endpoint URL.")
	f.StringVar(&o.secretNamespace, o.service+"-ca-secret-namespace", "", "Identity endpoint CA certificate secret namespace.")
	f.StringVar(&o.secretName, o.service+"-ca-secret-name", "", "Identity endpoint CA certificate secret.")
}

// ApplyTLSConfig adds CA certificates to the TLS  configuration if one is specified.
func (o *HTTPOptions) ApplyTLSConfig(ctx context.Context, cli client.Client, config *tls.Config) error {
	if o.secretName == "" {
		return nil
	}

	secret := &corev1.Secret{}

	if err := cli.Get(ctx, client.ObjectKey{Namespace: o.secretNamespace, Name: o.secretName}, secret); err != nil {
		return err
	}

	if secret.Type != corev1.SecretTypeTLS {
		return fmt.Errorf("%w: issuer CA not of type kubernetes.io/tls", errors.ErrSecretFormatError)
	}

	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return fmt.Errorf("%w: issuer CA missing tls.crt", errors.ErrSecretFormatError)
	}

	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(cert); !ok {
		return fmt.Errorf("%w: failed to load identity CA certificate", errors.ErrSecretFormatError)
	}

	config.RootCAs = certPool

	return nil
}

// HTTPClientOptions allows generic options to be passed to all HTTP clients.
type HTTPClientOptions struct {
	// secretNamespace tells us where to source the client certificate.
	secretNamespace string
	// secretName is the client certificate for the service.
	secretName string
	// reloadInterval determines how often the client certificate is reloaded.
	reloadInterval time.Duration
	now            func() time.Time
}

// AddFlags adds the options to the CLI flags.
func (o *HTTPClientOptions) AddFlags(f *pflag.FlagSet) {
	f.StringVar(&o.secretNamespace, "client-certificate-namespace", o.secretNamespace, "Client certificate secret namespace.")
	f.StringVar(&o.secretName, "client-certificate-name", o.secretName, "Client certificate secret name.")
	f.DurationVar(&o.reloadInterval, "client-certificate-reload-interval", defaultClientCertificateReloadInterval, "How often to check for a rotated client certificate. Zero or negative disables periodic reload.")
}

// SetNow overrides the clock used for inline client certificate reload checks.
func (o *HTTPClientOptions) SetNow(now func() time.Time) {
	o.now = now
}

func (o *HTTPClientOptions) clock() func() time.Time {
	if o.now != nil {
		return o.now
	}

	return time.Now
}

type tlsClientCertificateSource struct {
	mu sync.Mutex
	// current is the last successfully loaded certificate and is retained across reload failures.
	current *tls.Certificate
	// nextCheck bounds reload attempts to avoid reloading on every handshake.
	nextCheck      time.Time
	reloadInterval time.Duration
	now            func() time.Time
	loader         func() (*tls.Certificate, error)
}

type tlsClientCertificateReloader struct {
	options *HTTPClientOptions
	client  client.Client
}

func (r *tlsClientCertificateReloader) Load() (*tls.Certificate, error) {
	// Reloads happen on future TLS handshakes, so reusing the setup context would make
	// certificate refresh depend on whatever timeout/cancellation policy existed when
	// the transport was constructed. A background context keeps reloads independent of
	// that one-shot setup path. The tradeoff is that reloads are not currently time-bounded.
	return r.options.loadTLSCertificate(context.Background(), r.client)
}

func newTLSClientCertificateSource(reloadInterval time.Duration, now func() time.Time, loader func() (*tls.Certificate, error)) (*tlsClientCertificateSource, error) {
	certificate, err := loader()
	if err != nil {
		return nil, err
	}

	source := &tlsClientCertificateSource{
		current:        certificate,
		reloadInterval: reloadInterval,
		now:            now,
		loader:         loader,
	}

	if reloadInterval > 0 {
		source.nextCheck = now().Add(reloadInterval)
	}

	return source, nil
}

func (s *tlsClientCertificateSource) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reloadInterval <= 0 {
		return s.current, nil
	}

	if s.now().Before(s.nextCheck) {
		return s.current, nil
	}

	// Reload inline when the next handshake notices the check window has elapsed.
	certificate, err := s.loader()
	if err != nil {
		if s.current != nil {
			return s.current, nil
		}

		// The source preloads a certificate during construction, so this is a defensive
		// fallback for completeness rather than an expected runtime path.
		return nil, err
	}

	s.current = certificate
	s.nextCheck = s.now().Add(s.reloadInterval)

	return s.current, nil
}

func (o *HTTPClientOptions) loadTLSCertificate(ctx context.Context, cli client.Client) (*tls.Certificate, error) {
	secret := &corev1.Secret{}

	if err := cli.Get(ctx, client.ObjectKey{Namespace: o.secretNamespace, Name: o.secretName}, secret); err != nil {
		return nil, err
	}

	if secret.Type != corev1.SecretTypeTLS {
		return nil, fmt.Errorf("%w: certificate not of type kubernetes.io/tls", errors.ErrSecretFormatError)
	}

	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("%w: certificate missing tls.crt", errors.ErrSecretFormatError)
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("%w: certificate missing tls.key", errors.ErrSecretFormatError)
	}

	certificate, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}

	return &certificate, nil
}

// ApplyTLSClientConfig loads op a client certificate if one is configured and applies
// it to the provided TLS configuration.
func (o *HTTPClientOptions) ApplyTLSClientConfig(ctx context.Context, cli client.Client, config *tls.Config) error {
	if o.secretNamespace == "" || o.secretName == "" {
		return nil
	}

	reloader := &tlsClientCertificateReloader{
		options: o,
		client:  cli,
	}

	// Reloads happen during future TLS handshakes, so they must not depend on the setup context.
	source, err := newTLSClientCertificateSource(o.reloadInterval, o.clock(), reloader.Load)
	if err != nil {
		return err
	}

	config.GetClientCertificate = source.GetClientCertificate

	return nil
}

// EncodeAndSign takes an arbitrary data type, encodes as JSON, generates a digest and creates
// a digital signature, then returns a stringified version for verifiable communication from
// one service to another.  Confidentiality is ensured by the use of TLS.
func (o *HTTPClientOptions) EncodeAndSign(ctx context.Context, cli client.Client, data any) (string, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	certificate, err := o.loadTLSCertificate(ctx, cli)
	if err != nil {
		return "", err
	}

	// TODO: EC is equally valid and need support.
	pkey, ok := certificate.PrivateKey.(*rsa.PrivateKey)
	if !ok {
		return "", errors.ErrUnsupportedKeyType
	}

	signingKey := jose.SigningKey{
		Algorithm: jose.PS512,
		Key:       pkey,
	}

	signer, err := jose.NewSigner(signingKey, nil)
	if err != nil {
		return "", err
	}

	signedData, err := signer.Sign(dataJSON)
	if err != nil {
		return "", err
	}

	return signedData.CompactSerialize()
}

// VerifyAndDecode checks the payload's signature against the message and decodes the
// payload into an arbitrary data type.
func VerifyAndDecode(data any, payload string, certificate *x509.Certificate) error {
	signedData, err := jose.ParseSignedCompact(payload, []jose.SignatureAlgorithm{jose.PS512})
	if err != nil {
		return err
	}

	// TODO: EC is equally valid and need support.
	key, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.ErrUnsupportedKeyType
	}

	verifiedData, err := signedData.Verify(key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(verifiedData, data); err != nil {
		return err
	}

	return nil
}

// TLSClientConfig is a helper to create a TLS client configuration.
func TLSClientConfig(ctx context.Context, cli client.Client, options *HTTPOptions, clientOptions *HTTPClientOptions) (*tls.Config, error) {
	config := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	if err := options.ApplyTLSConfig(ctx, cli, config); err != nil {
		return nil, err
	}

	if err := clientOptions.ApplyTLSClientConfig(ctx, cli, config); err != nil {
		return nil, err
	}

	return config, nil
}
