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
	"sync"
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

type countingClient struct {
	crclient.Client

	gets       atomic.Int32
	blockOnGet int32
	getStarted chan struct{}
	releaseGet chan struct{}
	signalOnce sync.Once
}

func (c *countingClient) Get(ctx context.Context, key crclient.ObjectKey, obj crclient.Object, opts ...crclient.GetOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	count := c.gets.Add(1)

	if c.blockOnGet > 0 && count == c.blockOnGet {
		c.signalOnce.Do(func() {
			close(c.getStarted)
		})

		<-c.releaseGet
	}

	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *countingClient) GetCount() int {
	return int(c.gets.Load())
}

type staticClock struct {
	current time.Time
}

func newStaticClock() *staticClock {
	return &staticClock{current: time.Now()}
}

func (c *staticClock) Now() time.Time {
	return c.current
}

func (c *staticClock) Advance(d time.Duration) {
	c.current = c.current.Add(d)
}

func TestApplyTLSClientConfigUsesGetClientCertificate(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	config, client := mustTLSClientConfig(t, clock, time.Hour, mustTLSSecret(t, 1))

	require.Nil(t, config.Certificates)
	require.NotNil(t, config.GetClientCertificate)
	require.Equal(t, 1, client.GetCount())

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, serialNumber(t, certificate))
	require.Equal(t, 1, client.GetCount())
}

func TestApplyTLSClientConfigReloadsWhenDue(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	config, client := mustTLSClientConfig(t, clock, time.Hour, mustTLSSecret(t, 1))

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 2)))
	clock.Advance(time.Hour + time.Minute)

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, serialNumber(t, certificate))
	require.Equal(t, 2, client.GetCount())
}

func TestApplyTLSClientConfigReloadFailurePreservesCurrent(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	config, client := mustTLSClientConfig(t, clock, time.Hour, mustTLSSecret(t, 1))

	require.NoError(t, client.Delete(t.Context(), secretStub("client-cert")))
	clock.Advance(time.Hour + time.Minute)

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, serialNumber(t, certificate))
	require.Equal(t, 2, client.GetCount())
}

func TestApplyTLSClientConfigDisabledReloadKeepsCachedCertificate(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	config, client := mustTLSClientConfig(t, clock, 0, mustTLSSecret(t, 1))

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 2)))
	clock.Advance(24 * time.Hour)

	certificate, err := config.GetClientCertificate(nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, serialNumber(t, certificate))
	require.Equal(t, 1, client.GetCount())
}

func TestApplyTLSClientConfigConcurrentReloadIsSerialized(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	config, client := mustTLSClientConfig(t, clock, time.Hour, mustTLSSecret(t, 1))
	client.blockOnGet = 2
	client.getStarted = make(chan struct{})
	client.releaseGet = make(chan struct{})

	require.NoError(t, updateSecret(t, client.Client, mustTLSSecret(t, 2)))
	clock.Advance(time.Hour + time.Minute)

	var wg sync.WaitGroup

	results := make(chan int64, 8)
	errs := make(chan error, 8)

	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			certificate, err := config.GetClientCertificate(nil)
			if err != nil {
				errs <- err
				return
			}

			results <- serialNumber(t, certificate)
		}()
	}

	<-client.getStarted
	close(client.releaseGet)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	for serial := range results {
		require.EqualValues(t, 2, serial)
	}

	require.Equal(t, 2, client.GetCount())
}

func TestApplyTLSClientConfigInitialLoadFailure(t *testing.T) {
	t.Parallel()

	clock := newStaticClock()
	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	client := &countingClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
	}
	options := &coreclient.HTTPClientOptions{}

	flags := pflagSet(t, options)
	require.NoError(t, flags.Parse([]string{
		"--client-certificate-namespace=test",
		"--client-certificate-name=client-cert",
		"--client-certificate-reload-interval=1h",
	}))
	options.SetNow(clock.Now)

	config := &tls.Config{MinVersion: tls.VersionTLS13}

	err = options.ApplyTLSClientConfig(t.Context(), client, config)
	require.Error(t, err)
	require.Nil(t, config.GetClientCertificate)
	require.Equal(t, 1, client.GetCount())
}

func mustTLSClientConfig(t *testing.T, clock *staticClock, reloadInterval time.Duration, secret *corev1.Secret) (*tls.Config, *countingClient) {
	t.Helper()

	scheme, err := coreclient.NewScheme()
	require.NoError(t, err)

	baseClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	client := &countingClient{Client: baseClient}
	options := &coreclient.HTTPClientOptions{}

	flags := pflagSet(t, options)
	require.NoError(t, flags.Parse([]string{
		"--client-certificate-namespace=test",
		"--client-certificate-name=client-cert",
		"--client-certificate-reload-interval=" + reloadInterval.String(),
	}))
	options.SetNow(clock.Now)

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
