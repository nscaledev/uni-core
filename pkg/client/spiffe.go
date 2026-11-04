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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	certificateSourceSecret = "secret"
	certificateSourceSPIFFE = "spiffe"
)

var errSPIFFEConfiguration = errors.New("invalid SPIFFE client configuration")

// spiffeSource gives the SVID of the process and the trust bundles of the SPIFFE
// Workload API.  A test can give static sources.
type spiffeSource interface {
	x509svid.Source
	x509bundle.Source
	Close() error
}

// spiffeMode returns true when the client certificate comes from the SPIFFE
// Workload API.  Options with no flags are in Secret mode.
func (o *HTTPClientOptions) spiffeMode() (bool, error) {
	switch o.source {
	case "", certificateSourceSecret:
		return false, nil
	case certificateSourceSPIFFE:
		return true, nil
	}

	return false, fmt.Errorf("%w: --%s must be %s or %s, not %q", errSPIFFEConfiguration, o.flag("client-certificate-source"), certificateSourceSecret, certificateSourceSPIFFE, o.source)
}

// SetupSPIFFE opens the SPIFFE Workload API source in SPIFFE mode.  It waits at
// most the start timeout for the first SVID.  The socket address comes from
// SPIFFE_ENDPOINT_SOCKET.  In Secret mode it does nothing.
func (o *HTTPClientOptions) SetupSPIFFE(ctx context.Context) error {
	spiffe, err := o.spiffeMode()
	if err != nil {
		return err
	}

	if !spiffe {
		return nil
	}

	if o.secretName != "" || o.secretNamespace != "" {
		return fmt.Errorf("%w: --%s and --%s cannot be set with --%s=%s", errSPIFFEConfiguration, o.flag("client-certificate-name"), o.flag("client-certificate-namespace"), o.flag("client-certificate-source"), certificateSourceSPIFFE)
	}

	ctx, cancel := context.WithTimeout(ctx, o.spiffeStartTimeout)
	defer cancel()

	logger := log.Log.WithName("spiffe")

	source, err := workloadapi.NewX509Source(ctx, workloadapi.WithClientOptions(workloadapi.WithLogger(spiffeLogger{logger: logger})))
	if err != nil {
		return err
	}

	svid, err := source.GetX509SVID()
	if err != nil {
		return errors.Join(err, source.Close())
	}

	logger.Info("SVID source open", "spiffeID", svid.ID.String(), "notAfter", svid.Certificates[0].NotAfter)

	o.spiffe = source

	return nil
}

// Close closes the SVID source, if one is open.
func (o *HTTPClientOptions) Close() error {
	if o.spiffe == nil {
		return nil
	}

	return o.spiffe.Close()
}

// spiffeTLSClientConfig presents the SVID of the process, and accepts only a
// server with the SPIFFE ID of the endpoint.  Each handshake reads the current
// SVID and trust bundle from the source.
func spiffeTLSClientConfig(options *HTTPOptions, clientOptions *HTTPClientOptions) (*tls.Config, error) {
	source := clientOptions.spiffe
	if source == nil {
		return nil, fmt.Errorf("%w: the SVID source is not open, call SetupSPIFFE first", errSPIFFEConfiguration)
	}

	// The trust bundle replaces RootCAs, so a CA secret has no effect.
	if options.secretName != "" || options.secretNamespace != "" {
		return nil, fmt.Errorf("%w: --%s and --%s cannot be set with --%s=%s", errSPIFFEConfiguration, options.service+"-ca-secret-name", options.service+"-ca-secret-namespace", clientOptions.flag("client-certificate-source"), certificateSourceSPIFFE)
	}

	serverID, err := spiffeid.FromString(options.spiffeServerID)
	if err != nil {
		return nil, fmt.Errorf("%w: --%s: %w", errSPIFFEConfiguration, options.service+"-spiffe-server-id", err)
	}

	svid, err := source.GetX509SVID()
	if err != nil {
		return nil, err
	}

	if !serverID.MemberOf(svid.ID.TrustDomain()) {
		return nil, fmt.Errorf("%w: --%s %q is not in the trust domain %q of this process", errSPIFFEConfiguration, options.service+"-spiffe-server-id", serverID, svid.ID.TrustDomain())
	}

	// The host is the public endpoint, which does not present an SVID.
	if options.spiffeAddress == "" {
		return nil, fmt.Errorf("%w: --%s must be set with --%s=%s", errSPIFFEConfiguration, options.service+"-spiffe-address", clientOptions.flag("client-certificate-source"), certificateSourceSPIFFE)
	}

	config := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	tlsconfig.HookMTLSClientConfig(config, source, source, tlsconfig.AuthorizeID(serverID))

	return config, nil
}

// spiffeLogger sends the log of go-spiffe to the structured log.  Debug and
// warning lines become info lines, so the log shows when the Workload API watch
// starts.
type spiffeLogger struct {
	logger logr.Logger
}

func (l spiffeLogger) Debugf(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l spiffeLogger) Infof(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l spiffeLogger) Warnf(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l spiffeLogger) Errorf(format string, args ...any) {
	l.logger.Error(nil, fmt.Sprintf(format, args...))
}
