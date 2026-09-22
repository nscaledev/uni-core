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
	"sync/atomic"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/spf13/pflag"

	"github.com/unikorn-cloud/core/pkg/errors"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

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
	f.StringVar(&o.host, o.service+"-host", "", fmt.Sprintf("%s endpoint URL.", o.service))
	f.StringVar(&o.secretNamespace, o.service+"-ca-secret-namespace", "", fmt.Sprintf("%s endpoint CA certificate secret namespace.", o.service))
	f.StringVar(&o.secretName, o.service+"-ca-secret-name", "", fmt.Sprintf("%s endpoint CA certificate secret.", o.service))
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
	// service optionally prefixes the CLI flags.  The zero value is unprefixed.
	service string
	// secretNamespace tells us where to source the client certificate.
	secretNamespace string
	// secretName is the client certificate for the service.
	secretName string
}

// NewHTTPClientOptions returns options whose flags are prefixed with the service
// name, allowing a component to present a distinct client identity per peer.
// The zero value is unprefixed and MUST retain the original flag names, as every
// caller that needs only one client identity declares the type by value.
func NewHTTPClientOptions(service string) *HTTPClientOptions {
	return &HTTPClientOptions{
		service: service,
	}
}

// flag applies the service prefix to a flag name, if there is one.
func (o *HTTPClientOptions) flag(name string) string {
	if o.service == "" {
		return name
	}

	return o.service + "-" + name
}

// AddFlags adds the options to the CLI flags.
func (o *HTTPClientOptions) AddFlags(f *pflag.FlagSet) {
	f.StringVar(&o.secretNamespace, o.flag("client-certificate-namespace"), o.secretNamespace, "Client certificate secret namespace.")
	f.StringVar(&o.secretName, o.flag("client-certificate-name"), o.secretName, "Client certificate secret name.")

	// Accepted and ignored: the certificate is reloaded on every handshake.  Deployments
	// still pass this, and pflag rejects unknown flags, so removing it stops those pods
	// starting.  Delete once no chart emits it.
	//
	// Unprefixed options only.  No deployment passes a prefixed form, and registering one
	// per prefix would redefine this flag, which pflag panics on, in any process holding
	// both a prefixed and an unprefixed client.
	if o.service == "" {
		f.Duration("client-certificate-reload-interval", 0, "Deprecated: ignored, the client certificate is reloaded on every handshake.")

		if err := f.MarkDeprecated("client-certificate-reload-interval", "the client certificate is reloaded on every handshake"); err != nil {
			panic(err)
		}
	}
}

// tlsClientCertificateSource loads the client certificate for every handshake,
// so a rotated CA or leaf takes effect without a restart.  MUST NOT cache on a
// timer: a stale certificate is refused by the ingress once trust moves.  See
// the README's client certificate section.
type tlsClientCertificateSource struct {
	// current is the last successfully loaded certificate, served only when a
	// reload fails so a transient read error does not fail the handshake.
	current atomic.Pointer[tls.Certificate]
	// lastFailureLog rate limits the failure log, as unix nanoseconds.  A Secret that
	// cannot be read fails on every handshake, so logging each one floods.  An edge
	// flag is not enough: an intermittent failure flips it every handshake, and two
	// concurrent handshakes can apply their updates out of order and leave it set
	// while healthy, swallowing the next genuine failure.
	lastFailureLog atomic.Int64
	// secret identifies this source in logs, as namespace/name.
	secret string
	load   func(context.Context) (*tls.Certificate, error)
}

// clientCertificateFailureLogInterval is how often a sustained reload failure is
// repeated in the log.  Silence would hide a service running on a certificate the
// ingress will reject at the next rotation.
const clientCertificateFailureLogInterval = time.Minute

// clientCertificateLoadTimeout bounds a single load.  The Secret is normally read
// from the controller-runtime cache and returns immediately, but callers may pass an
// uncached client where this is an API round trip.  See the README.
const clientCertificateLoadTimeout = 5 * time.Second

// certificateLoader returns the function the source calls for each load.  The caller
// supplies the context: the setup context at construction, and the handshake's own
// context thereafter, so a caller that gives up waiting is not held here.  The load is
// additionally capped, or a slow read with no deadline of its own hangs the handshake.
func (o *HTTPClientOptions) certificateLoader(cli client.Client) func(context.Context) (*tls.Certificate, error) {
	return func(ctx context.Context) (*tls.Certificate, error) {
		ctx, cancel := context.WithTimeout(ctx, clientCertificateLoadTimeout)
		defer cancel()

		return o.loadTLSCertificate(ctx, cli)
	}
}

func newTLSClientCertificateSource(ctx context.Context, secret string, load func(context.Context) (*tls.Certificate, error)) (*tlsClientCertificateSource, error) {
	certificate, err := load(ctx)
	if err != nil {
		return nil, err
	}

	source := &tlsClientCertificateSource{
		secret: secret,
		load:   load,
	}

	source.current.Store(certificate)

	return source, nil
}

// logFailure reports a reload failure at most once per interval.  Rate limiting
// rather than edge triggering, for the reasons on lastFailureLog.
func (s *tlsClientCertificateSource) logFailure(err error) {
	now := time.Now().UnixNano()
	last := s.lastFailureLog.Load()

	if last != 0 && now-last < int64(clientCertificateFailureLogInterval) {
		return
	}

	// Lost the race, so another handshake is logging this same failure.
	if !s.lastFailureLog.CompareAndSwap(last, now) {
		return
	}

	log.Log.Error(err, "client certificate reload failed, serving the last loaded certificate", "secret", s.secret)
}

// GetClientCertificate reloads on every handshake.  The load MUST NOT be held
// under a lock: parsing an RSA 4096 key pair costs around a millisecond, which
// would serialize concurrent handshakes.  See the README for the measurements.
func (s *tlsClientCertificateSource) GetClientCertificate(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	// Honour the handshake's own deadline.  Without it a caller that has already
	// given up still waits here for the full load timeout, on its own goroutine.
	ctx := context.Background()
	if request != nil && request.Context() != nil {
		ctx = request.Context()
	}

	certificate, err := s.load(ctx)
	if err != nil {
		s.logFailure(err)

		if current := s.current.Load(); current != nil {
			return current, nil
		}

		// The source preloads a certificate during construction, so this is a defensive
		// fallback for completeness rather than an expected runtime path.
		return nil, err
	}

	// Concurrent handshakes across a rotation can store an older certificate after a
	// newer one.  Every handshake returns what it loaded itself, and only the failure
	// fallback reads this, so the cost is that the fallback may be one generation
	// stale until the next successful load.  MUST NOT be ordered with a lock, which
	// would serialize handshakes.
	s.current.Store(certificate)

	return certificate, nil
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

	source, err := newTLSClientCertificateSource(ctx, o.secretNamespace+"/"+o.secretName, o.certificateLoader(cli))
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
