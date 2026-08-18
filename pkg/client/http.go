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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/spf13/pflag"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"

	"github.com/unikorn-cloud/core/pkg/errors"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultClientCertificateReloadInterval = 24 * time.Hour

// Certificate sources for --client-certificate-source.
const (
	CertificateSourceSecret = "secret"
	CertificateSourceSPIFFE = "spiffe"
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
	// certificateSource selects where the client credential comes from.
	certificateSource string
	// spiffeServerID is the SPIFFE ID the server must present.  Required in SPIFFE mode;
	// InitSPIFFE and ApplyTLSClientConfig both reject the empty value there.
	spiffeServerID string
	// spiffeSources is the live Workload API connection, set by InitSPIFFE.
	spiffeSources Sources
	// newSources opens the Workload API connection.  Nil means NewSPIFFESources, which
	// is what production always uses; an internal test substitutes a static pair so
	// that "flags in, usable credential out" can be asserted without a live agent
	// socket.
	newSources func(ctx context.Context) (Sources, io.Closer, error)
}

// AddFlags adds the options to the CLI flags.
func (o *HTTPClientOptions) AddFlags(f *pflag.FlagSet) {
	f.StringVar(&o.secretNamespace, "client-certificate-namespace", o.secretNamespace, "Client certificate secret namespace.")
	f.StringVar(&o.secretName, "client-certificate-name", o.secretName, "Client certificate secret name.")
	f.DurationVar(&o.reloadInterval, "client-certificate-reload-interval", defaultClientCertificateReloadInterval, "How often to check for a rotated client certificate. Zero or negative disables periodic reload.")
	f.StringVar(&o.certificateSource, "client-certificate-source", CertificateSourceSecret, "Where the client credential comes from: secret or spiffe.  In spiffe mode the Workload API address comes from SPIFFE_ENDPOINT_SOCKET.")
	f.StringVar(&o.spiffeServerID, "spiffe-server-id", "", "SPIFFE ID the server must present.  Required in spiffe mode: the client authorizes only this exact ID, and startup fails if it is unset.")
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
	if o.certificateSource == CertificateSourceSPIFFE {
		if o.spiffeSources == nil {
			return fmt.Errorf("%w: SPIFFE sources not initialised", ErrClientCredential)
		}

		// InitSPIFFE rejects an empty --spiffe-server-id at startup.  This check is
		// defence in depth for a caller that sets spiffeSources directly without going
		// through InitSPIFFE: an empty ID must never fall back to authorizing any peer.
		if o.spiffeServerID == "" {
			return fmt.Errorf("%w: --spiffe-server-id is required in spiffe mode", ErrClientCredential)
		}

		id, err := spiffeid.FromString(o.spiffeServerID)
		if err != nil {
			return fmt.Errorf("%w: parsing --spiffe-server-id: %w", ErrClientCredential, err)
		}

		// HookMTLSClientConfig sets InsecureSkipVerify: true.  That is not a weakening.
		// A SPIFFE ID lives in a URI SAN, so hostname verification would always fail;
		// go-spiffe replaces it with VerifyPeerCertificate, which checks the chain
		// against the bundle and then authorizes the SPIFFE ID.  An error there aborts
		// the handshake, so verification is relocated rather than skipped.  See
		// go-spiffe spiffetls/tlsconfig/config.go:187.
		//
		// It also resets config.RootCAs to nil (resetAuthFields, config.go:244),
		// discarding whatever ApplyTLSConfig set above from --identity-ca-secret-name.
		// That is deliberate: in SPIFFE mode the peer is verified by SPIFFE ID against
		// the trust bundle instead of by PKI chain validation, so the CA secret becomes
		// a no-op here.  MinVersion survives the reset -- resetAuthFields only raises it
		// to at least TLS 1.2 and never lowers it, and TLSClientConfig already sets TLS
		// 1.3 before either function runs (confirmed against go-spiffe v2.8.1 source).
		// The consequence: a SPIFFE-mode client can only talk to a SPIFFE-serving
		// endpoint, with no fallback to a PKI-terminated one.  A component migrates its
		// credential source and its target host together, or not at all.
		tlsconfig.HookMTLSClientConfig(config, o.spiffeSources, o.spiffeSources, tlsconfig.AuthorizeID(id))

		return nil
	}

	if o.secretNamespace == "" || o.secretName == "" {
		return nil
	}

	source, err := o.credentialSource(cli)
	if err != nil {
		return err
	}

	// Reloads happen during future TLS handshakes, so they must not depend on the setup context.
	//nolint:contextcheck // reloads intentionally use a background context rather than the setup context above.
	reloading, err := newTLSClientCertificateSource(o.reloadInterval, o.clock(), func() (*tls.Certificate, error) {
		return source.Certificate(context.Background())
	})
	if err != nil {
		return err
	}

	config.GetClientCertificate = reloading.GetClientCertificate

	return nil
}

// credentialSource returns the source the configured flags select.
func (o *HTTPClientOptions) credentialSource(cli client.Client) (credentialSource, error) {
	switch o.certificateSource {
	case CertificateSourceSecret, "":
		return &secretCredentialSource{options: o, client: cli}, nil
	case CertificateSourceSPIFFE:
		if o.secretName != "" || o.secretNamespace != "" {
			return nil, fmt.Errorf("%w: --client-certificate-source=spiffe conflicts with --client-certificate-name/--client-certificate-namespace; set one or the other", ErrClientCredential)
		}

		if o.spiffeSources == nil {
			return nil, fmt.Errorf("%w: SPIFFE sources not initialised; call InitSPIFFE before building clients", ErrClientCredential)
		}

		return SPIFFECredential(o.spiffeSources), nil
	default:
		return nil, fmt.Errorf("%w: unknown --client-certificate-source %q", ErrClientCredential, o.certificateSource)
	}
}

// noopCloser stands in when SPIFFE mode is off, so callers can defer Close
// unconditionally.
type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// NewSPIFFESources opens a Workload API connection.  The address comes from
// SPIFFE_ENDPOINT_SOCKET, which is the variable the SPIFFE specification defines and
// the csi.spiffe.io driver sets.
//
// The returned source streams rotations for the process's lifetime, so its Close must
// not run until every consumer is done with it.
func NewSPIFFESources(ctx context.Context) (Sources, io.Closer, error) {
	source, err := workloadapi.NewX509Source(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: connecting to the Workload API: %w", ErrClientCredential, err)
	}

	return source, source, nil
}

// InitSPIFFE validates the certificate source and, in SPIFFE mode, opens the Workload
// API connection; in Secret mode it is a no-op.  Call it once at startup, before any
// client is built, so a misconfiguration fails loudly there instead of silently at
// first use.
//
// Mode-string validation happens before the Secret-mode early return, so an unknown
// --client-certificate-source is rejected regardless of mode.  The SPIFFE-mode checks
// below matter because ApplyTLSClientConfig's SPIFFE branch never calls
// credentialSource: without them, a client that never signs a principal (so never
// reaches credentialSource's own conflict check) would silently ignore stray
// --client-certificate-name/--client-certificate-namespace flags, or run with no server
// authorization at all if --spiffe-server-id were left unset.
func (o *HTTPClientOptions) InitSPIFFE(ctx context.Context) (io.Closer, error) {
	switch o.certificateSource {
	case CertificateSourceSecret, "":
		return noopCloser{}, nil
	case CertificateSourceSPIFFE:
	default:
		return nil, fmt.Errorf("%w: unknown --client-certificate-source %q", ErrClientCredential, o.certificateSource)
	}

	if o.secretName != "" || o.secretNamespace != "" {
		return nil, fmt.Errorf("%w: --client-certificate-source=spiffe conflicts with --client-certificate-name/--client-certificate-namespace; set one or the other", ErrClientCredential)
	}

	if o.spiffeServerID == "" {
		return nil, fmt.Errorf("%w: --spiffe-server-id is required in spiffe mode", ErrClientCredential)
	}

	newSources := o.newSources
	if newSources == nil {
		newSources = NewSPIFFESources
	}

	sources, closer, err := newSources(ctx)
	if err != nil {
		return nil, err
	}

	o.spiffeSources = sources

	return closer, nil
}

// signedPrincipalAlgorithms are the algorithms accepted when verifying a signed payload.  RSA
// covers certificates issued by cert-manager, and ECDSA those issued by SPIRE, which uses
// EC P-256 for workload X509-SVIDs by default.  Verification must accept both for as long as
// either issuer is in use, and must accept the new one before anything starts producing it.
func signedPrincipalAlgorithms() []jose.SignatureAlgorithm {
	return []jose.SignatureAlgorithm{
		jose.PS512,
		jose.ES256,
		jose.ES384,
	}
}

// signatureAlgorithm selects the signing algorithm for a key.  JOSE binds each ECDSA curve to
// exactly one algorithm, so the curve decides rather than the caller.
func signatureAlgorithm(key crypto.PrivateKey) (jose.SignatureAlgorithm, error) {
	switch k := key.(type) {
	case *rsa.PrivateKey:
		return jose.PS512, nil
	case *ecdsa.PrivateKey:
		switch k.Curve {
		case elliptic.P256():
			return jose.ES256, nil
		case elliptic.P384():
			return jose.ES384, nil
		}

		return "", fmt.Errorf("%w: unsupported elliptic curve %s", errors.ErrUnsupportedKeyType, k.Curve.Params().Name)
	}

	return "", errors.ErrUnsupportedKeyType
}

// EncodeAndSign takes an arbitrary data type, encodes as JSON, generates a digest and creates
// a digital signature, then returns a stringified version for verifiable communication from
// one service to another.  Confidentiality is ensured by the use of TLS.
func (o *HTTPClientOptions) EncodeAndSign(ctx context.Context, cli client.Client, data any) (string, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	source, err := o.credentialSource(cli)
	if err != nil {
		return "", err
	}

	certificate, err := source.Certificate(ctx)
	if err != nil {
		return "", err
	}

	algorithm, err := signatureAlgorithm(certificate.PrivateKey)
	if err != nil {
		return "", err
	}

	signingKey := jose.SigningKey{
		Algorithm: algorithm,
		Key:       certificate.PrivateKey,
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
	signedData, err := jose.ParseSignedCompact(payload, signedPrincipalAlgorithms())
	if err != nil {
		return err
	}

	switch certificate.PublicKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
	default:
		return errors.ErrUnsupportedKeyType
	}

	verifiedData, err := signedData.Verify(certificate.PublicKey)
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
