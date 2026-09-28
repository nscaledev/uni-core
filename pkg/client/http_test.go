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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"

	coreclient "github.com/unikorn-cloud/core/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mirrors clientCertificateLoadTimeout, which is unexported.  If the production
// constant changes this must too, and TestHandshakeContextBoundsTheLoad is the
// assertion that would start passing vacuously if it did not.
const clientCertificateLoadTimeoutForTest = 5 * time.Second

type countingClient struct {
	crclient.Client

	gets atomic.Int32
}

func (c *countingClient) Get(ctx context.Context, key crclient.ObjectKey, obj crclient.Object, opts ...crclient.GetOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.gets.Add(1)

	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *countingClient) GetCount() int {
	return int(c.gets.Load())
}

// barrierClient blocks every Get until the test releases it, so a test can
// observe how many loads are in flight at once.  A serialized implementation
// only ever has one.
type barrierClient struct {
	crclient.Client

	armed    atomic.Bool
	arrivals chan struct{}
	release  chan struct{}
}

func (c *barrierClient) arm() {
	c.armed.Store(true)
}

func (c *barrierClient) Get(ctx context.Context, key crclient.ObjectKey, obj crclient.Object, opts ...crclient.GetOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// The construction-time load must not block, only the handshakes under test.
	if c.armed.Load() {
		c.arrivals <- struct{}{}
		<-c.release
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

// deadlineClient records whether each Get carried a deadline.
type deadlineClient struct {
	crclient.Client

	deadlines chan bool
	budgets   chan time.Duration
}

func (c *deadlineClient) Get(ctx context.Context, key crclient.ObjectKey, obj crclient.Object, opts ...crclient.GetOption) error {
	deadline, ok := ctx.Deadline()
	c.deadlines <- ok

	if ok {
		select {
		case c.budgets <- time.Until(deadline):
		default:
		}
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

func TestGetClientCertificateIsUsedInsteadOfStaticCertificates(t *testing.T) {
	t.Parallel()

	config, client := mustTLSClientConfig(t, mustTLSSecret(t, 1))

	require.Nil(t, config.Certificates)
	require.NotNil(t, config.GetClientCertificate)

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, serialNumber(t, certificate))

	// One read at construction, one for the handshake.  The construction read is
	// what makes a missing Secret fail at startup rather than at first request.
	require.Equal(t, 2, client.GetCount())
}

// A rotated certificate must be picked up by the very next handshake, with no
// restart and no waiting for a reload interval to elapse.
func TestGetClientCertificateReloadsOnEveryHandshake(t *testing.T) {
	t.Parallel()

	config, client := mustTLSClientConfig(t, mustTLSSecret(t, 1))

	before := client.GetCount()

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 2)))

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, serialNumber(t, certificate))
	require.Equal(t, before+1, client.GetCount())

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 3)))

	certificate, err = config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 3, serialNumber(t, certificate))
	require.Equal(t, before+2, client.GetCount())
}

// A transient read failure must serve the last good certificate rather than
// failing the handshake, and must retry on the handshake after that.
func TestGetClientCertificateFailureServesLastGoodCertificate(t *testing.T) {
	t.Parallel()

	config, client := mustTLSClientConfig(t, mustTLSSecret(t, 1))

	require.NoError(t, client.Delete(t.Context(), secretStub("client-cert")))

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, serialNumber(t, certificate))

	// Recovery: once the secret is back, the next handshake picks it up.
	require.NoError(t, client.Create(t.Context(), mustTLSSecret(t, 2)))

	certificate, err = config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, serialNumber(t, certificate))
}

// Parsing an RSA 4096 key pair costs around a millisecond, so handshakes must
// not queue behind one another.  If the load is held under a lock this test
// only ever sees one arrival and times out.
func TestGetClientCertificateDoesNotSerializeHandshakes(t *testing.T) {
	t.Parallel()

	const handshakes = 4

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mustTLSSecret(t, 1)).Build()
	options := mustHTTPClientOptions(t)

	blocking := &barrierClient{
		Client:   base,
		arrivals: make(chan struct{}, handshakes),
		release:  make(chan struct{}),
	}

	config := &tls.Config{MinVersion: tls.VersionTLS13}
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), blocking, config))

	blocking.arm()

	errs := make(chan error, handshakes)

	for range handshakes {
		go func() {
			_, err := config.GetClientCertificate(nil)
			errs <- err
		}()
	}

	for range handshakes {
		select {
		case <-blocking.arrivals:
		// Deliberately shorter than the load timeout, so a slow runner cannot make
		// an expiring load look like serialization.
		case <-time.After(2 * time.Second):
			t.Fatal("handshakes are serialized: fewer concurrent loads than handshakes")
		}
	}

	close(blocking.release)

	for range handshakes {
		require.NoError(t, <-errs)
	}
}

// Deployments still pass --client-certificate-reload-interval, so the flag must
// keep parsing or those pods stop starting.  It must no longer gate reloads.
func TestReloadIntervalFlagIsAcceptedButIgnored(t *testing.T) {
	t.Parallel()

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mustTLSSecret(t, 1)).Build()
	client := &countingClient{Client: baseClient}

	options := &coreclient.HTTPClientOptions{}

	flags := pflagSet(t, options)
	require.NoError(t, flags.Parse([]string{
		"--client-certificate-namespace=test",
		"--client-certificate-name=client-cert",
		"--client-certificate-reload-interval=24h",
	}))

	config := &tls.Config{MinVersion: tls.VersionTLS13}
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), client, config))

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 2)))

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, serialNumber(t, certificate))
}

func TestApplyTLSClientConfigInitialLoadFailure(t *testing.T) {
	t.Parallel()

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	client := &countingClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}

	options := mustHTTPClientOptions(t)

	config := &tls.Config{MinVersion: tls.VersionTLS13}

	err = options.ApplyTLSClientConfig(t.Context(), client, config)
	require.Error(t, err)
	require.Nil(t, config.GetClientCertificate)
	require.Equal(t, 1, client.GetCount())
}

// The reload deliberately does not inherit the setup context, but it MUST still
// be bounded.  Callers may pass an uncached client, where the read is an API
// round trip, and an unbounded one would hang the handshake indefinitely.
func TestReloadContextIsBounded(t *testing.T) {
	t.Parallel()

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mustTLSSecret(t, 1)).Build()
	client := &deadlineClient{Client: base, deadlines: make(chan bool, 8), budgets: make(chan time.Duration, 8)}

	options := mustHTTPClientOptions(t)

	config := &tls.Config{MinVersion: tls.VersionTLS13}
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), client, config))

	require.True(t, <-client.deadlines, "construction load must be time bounded")

	_, err = config.GetClientCertificate(nil)
	require.NoError(t, err)

	require.True(t, <-client.deadlines, "handshake reload must be time bounded")
}

// A reload that fails serves the last good certificate, which is silent by
// design.  It MUST still be visible: an unreadable Secret means the service is
// running on a certificate that the ingress will reject at the next rotation.
// Logging every handshake would flood, so the failure is reported at most once
// per interval.
func TestReloadFailureLogIsRateLimited(t *testing.T) {
	t.Parallel()

	const secretName = "ratelimited-client"

	// The recorder outlives a single run, so clear this key or the assertions
	// below see the previous run's lines under -count.
	recorder.forget("test/" + secretName)

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	secret := mustTLSSecret(t, 1)
	secret.Name = secretName

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	options := mustNamedHTTPClientOptions(t, secretName)

	config := &tls.Config{MinVersion: tls.VersionTLS13}
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), base, config))

	// A successful handshake, not just construction, must be silent.
	_, err = config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.Empty(t, recorder.messagesFor("test/"+secretName), "a healthy handshake must log nothing")

	// Break it and handshake repeatedly: the failure is reported once per interval,
	// not once per handshake, and not once per flap.
	require.NoError(t, base.Delete(t.Context(), secretStub(secretName)))

	for range 4 {
		certificate, err := config.GetClientCertificate(nil)
		require.NoError(t, err)
		require.EqualValues(t, 1, serialNumber(t, certificate), "the last loaded certificate is still served")
	}

	require.Len(t, recorder.messagesFor("test/"+secretName), 1, "a sustained failure logs once per interval")

	// Flapping must not turn the rate limit back into one line per handshake.
	recovered := mustTLSSecret(t, 2)
	recovered.Name = secretName
	require.NoError(t, base.Create(t.Context(), recovered))

	_, err = config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.NoError(t, base.Delete(t.Context(), secretStub(secretName)))

	for range 4 {
		_, err = config.GetClientCertificate(nil)
		require.NoError(t, err)
	}

	require.Len(t, recorder.messagesFor("test/"+secretName), 1, "flapping must not defeat the rate limit")
}

// A real handshake, because CertificateRequestInfo's context cannot be constructed
// from outside crypto/tls.  The load must inherit the handshake's deadline, so a
// caller that has given up is not held here for the full load timeout.
func TestHandshakeContextBoundsTheLoad(t *testing.T) {
	t.Parallel()

	const (
		secretName   = "handshake-cert"
		callerBudget = 1500 * time.Millisecond
	)

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	secret := mustTLSSecret(t, 1)
	secret.Name = secretName

	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	client := &deadlineClient{Client: base, deadlines: make(chan bool, 8), budgets: make(chan time.Duration, 8)}

	options := mustNamedHTTPClientOptions(t, secretName)

	clientConfig := &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true} //nolint:gosec // a pipe to a throwaway server in-process.
	require.NoError(t, options.ApplyTLSClientConfig(t.Context(), client, clientConfig))

	// Drain the construction load, which legitimately uses the setup context.
	<-client.deadlines
	<-client.budgets

	serverCertPEM, serverKeyPEM := mustIssueTLSKeyPair(t, 99)
	serverCertificate, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	require.NoError(t, err)

	clientPipe, serverPipe := net.Pipe()

	defer clientPipe.Close()
	defer serverPipe.Close()

	go func() {
		server := tls.Server(serverPipe, &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{serverCertificate},
			ClientAuth:   tls.RequestClientCert,
		})
		_ = server.HandshakeContext(t.Context())
	}()

	ctx, cancel := context.WithTimeout(t.Context(), callerBudget)
	defer cancel()

	_ = tls.Client(clientPipe, clientConfig).HandshakeContext(ctx)

	require.True(t, <-client.deadlines, "the handshake load must be bounded")

	budget := <-client.budgets
	require.Less(t, budget, callerBudget, "the load must inherit the caller's deadline")
	require.Less(t, budget, clientCertificateLoadTimeoutForTest,
		"the load must not fall back to the flat load timeout")
}

func mustHTTPClientOptions(t *testing.T) *coreclient.HTTPClientOptions {
	t.Helper()

	return mustNamedHTTPClientOptions(t, "client-cert")
}

func mustNamedHTTPClientOptions(t *testing.T, name string) *coreclient.HTTPClientOptions {
	t.Helper()

	options := &coreclient.HTTPClientOptions{}

	flags := pflagSet(t, options)
	require.NoError(t, flags.Parse([]string{
		"--client-certificate-namespace=test",
		"--client-certificate-name=" + name,
	}))

	return options
}

func mustTLSClientConfig(t *testing.T, secret *corev1.Secret) (*tls.Config, *countingClient) {
	t.Helper()

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	client := &countingClient{Client: baseClient}
	options := mustHTTPClientOptions(t)

	config := &tls.Config{MinVersion: tls.VersionTLS13}

	err = options.ApplyTLSClientConfig(t.Context(), client, config)
	require.NoError(t, err)

	return config, client
}

func pflagSet(t *testing.T, options *coreclient.HTTPClientOptions) *pflag.FlagSet {
	t.Helper()

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options.AddFlags(flags)

	return flags
}

func updateSecret(t *testing.T, client crclient.Client, secret *corev1.Secret) error {
	t.Helper()

	current := &corev1.Secret{}
	key := crclient.ObjectKeyFromObject(secret)

	if err := client.Get(t.Context(), key, current); err != nil {
		return err
	}

	current.Type = secret.Type
	current.Data = secret.Data

	return client.Update(t.Context(), current)
}

func secretStub(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      name,
		},
	}
}

func mustTLSSecret(t *testing.T, serial int64) *corev1.Secret {
	t.Helper()

	certPEM, keyPEM := mustIssueTLSKeyPair(t, serial)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			Name:      "client-cert",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certPEM,
			corev1.TLSPrivateKeyKey: keyPEM,
		},
	}
}

func mustIssueTLSKeyPair(t *testing.T, serial int64) ([]byte, []byte) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			CommonName: "client",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	return certificatePEM, keyPEM
}

func serialNumber(t *testing.T, certificate *tls.Certificate) int64 {
	t.Helper()

	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	require.NoError(t, err)

	return leaf.SerialNumber.Int64()
}
