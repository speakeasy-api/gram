package assistants

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestKitGKEGatewayClientTLS(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"trusted", "untrusted certificate", "wrong hostname", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			const name = "runtime-test.kit.internal"
			certPEM, keyPEM, err := newKitGKETLSCertificate(name)
			require.NoError(t, err)
			trustedPEM := certPEM
			if scenario == "untrusted certificate" {
				certPEM, keyPEM, err = newKitGKETLSCertificate(name)
				require.NoError(t, err)
			}
			if scenario == "wrong hostname" {
				certPEM, keyPEM, err = newKitGKETLSCertificate("other-runtime.kit.internal")
				require.NoError(t, err)
			}
			pair, err := tls.X509KeyPair(certPEM, keyPEM)
			require.NoError(t, err)
			var received, redirected atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				if r.URL.Path == "/redirected" {
					redirected.Add(1)
				}
				if r.Header.Get("Authorization") != "Bearer test-runtime-token" {
					t.Error("missing expected runtime bearer")
				}
				if scenario == "redirect" && r.URL.Path == "/" {
					http.Redirect(w, r, "/redirected", http.StatusTemporaryRedirect)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
			server.StartTLS()
			t.Cleanup(server.Close)
			policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
			client, err := kitGKEGatewayClient(policy, []string{"127.0.0.1/32"}, trustedPEM, name)
			require.NoError(t, err)
			t.Cleanup(client.CloseIdleConnections)
			if scenario == "wrong hostname" {
				transport, ok := client.Transport.(*http.Transport)
				require.True(t, ok)
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
				require.True(t, transport.TLSClientConfig.RootCAs.AppendCertsFromPEM(certPEM))
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer test-runtime-token")
			response, err := client.Do(request)
			if scenario == "untrusted certificate" || scenario == "wrong hostname" {
				require.Error(t, err)
				if scenario == "wrong hostname" {
					var hostnameError x509.HostnameError
					require.ErrorAs(t, err, &hostnameError)
				}
				require.Nil(t, response)
				require.Zero(t, received.Load(), "bearer must not reach an untrusted endpoint")
				return
			}
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.EqualValues(t, 1, received.Load())
			require.Zero(t, redirected.Load())
			if scenario == "redirect" {
				require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
			} else {
				require.Equal(t, http.StatusNoContent, response.StatusCode)
			}
		})
	}
}

// No credential store is installed: a plaintext route must fail before loading
// or sending the gateway bearer, rather than relying only on renderer choices.
func TestKitGKEProtocolFactoryRejectsPlaintext(t *testing.T) {
	t.Parallel()
	core := &ServiceCore{guardianPolicy: guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))}
	factory := core.kitProtocolFactory(KitLocalCompositionConfig{}, []string{"127.0.0.1/32"})
	for _, endpoint := range []*url.URL{nil, {Scheme: "http", Host: "127.0.0.1:8081", Path: "/acp/v2"}} {
		protocol, err := factory(t.Context(), assistantRuntimeRecord{Backend: runtimeBackendGKE}, endpoint)
		require.ErrorIs(t, err, ErrKitRuntimeBinding)
		require.Nil(t, protocol)
	}
}
