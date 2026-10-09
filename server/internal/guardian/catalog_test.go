package guardian

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// Internal tests cannot import testenv: its identity fixtures import guardian.
// Catalog destinations in these fixtures are synthetic private addresses.
func TestCatalogConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cidr  string
		valid bool
	}{
		{"", true}, {"10.23.45.67/32", true}, {"10.23.45.67", false},
		{"10.10.0.0/24", false}, {"0.0.0.0/0", false}, {"127.0.0.1/32", false},
		{"8.8.8.8/32", false}, {"fd00::1/128", false}, {"invalid", false},
	} {
		t.Run(tc.cidr, func(t *testing.T) {
			t.Parallel()
			_, err := WithInternalCatalogCIDR(tc.cidr)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCatalogURLValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		url   string
		ips   []string
		valid bool
	}{
		{"approved", "https://alpha.catalog.dev.speakeasy.com/okta/mcp", []string{"10.23.45.67"}, true},
		{"second label", "https://beta.catalog.dev.speakeasy.com/okta/mcp", []string{"10.23.45.67"}, true},
		{"normalized", "https://ALPHA.CATALOG.DEV.SPEAKEASY.COM.:443/okta/mcp", []string{"10.23.45.67"}, true},
		{"alternate hostname", "https://alternate.example.test/mcp", []string{"10.23.45.67"}, true},
		{"literal", "https://10.23.45.67/mcp", nil, true},
		{"mapped literal", "https://[::ffff:10.23.45.67]/mcp", nil, true},
		{"http", "http://alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, true},
		{"other port", "https://alpha.catalog.dev.speakeasy.com:8443/mcp", []string{"10.23.45.67"}, true},
		{"other private IP", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.68"}, false},
		{"public IP", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"8.8.8.8"}, true},
		{"mixed DNS with private IP", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67", "10.23.45.68"}, false},
		{"empty DNS", "https://alpha.catalog.dev.speakeasy.com/mcp", nil, false},
		{"ordinary public", "https://public.example.com/mcp", []string{"8.8.8.8"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
				var ips []net.IP
				for _, ip := range tc.ips {
					ips = append(ips, net.ParseIP(ip))
				}
				return ips, nil
			}})
			option, err := WithInternalCatalogCIDR("10.23.45.67/32")
			require.NoError(t, err)
			policy := NewDefaultPolicy(noop.NewTracerProvider(), option, WithResolver(resolver)) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
			_, err = policy.ValidateHTTPURL(t.Context(), tc.url, WithInternalCatalog())
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCatalogRequiresConfigurationAndClientOptIn(t *testing.T) {
	t.Parallel()
	for _, configured := range []bool{false, true} {
		for _, optedIn := range []bool{false, true} {
			if configured && optedIn {
				continue
			}
			t.Run(fmt.Sprintf("configured=%t/optedIn=%t", configured, optedIn), func(t *testing.T) {
				t.Parallel()
				resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
					return []net.IP{net.ParseIP("10.23.45.67")}, nil
				}})
				policy := NewDefaultPolicy(noop.NewTracerProvider(), WithResolver(resolver)) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
				if configured {
					option, err := WithInternalCatalogCIDR("10.23.45.67/32")
					require.NoError(t, err)
					option(policy)
				}
				var options []ClientOption
				if optedIn {
					options = append(options, WithInternalCatalog())
				}
				endpoint := "https://alpha.catalog.dev.speakeasy.com/okta/mcp"
				_, err := policy.ValidateHTTPURL(t.Context(), endpoint, options...)
				require.ErrorIs(t, err, ErrBlockedIP)
				client := policy.Client(options...)
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, endpoint, nil)
				require.NoError(t, err)
				resp, err := client.Do(req)
				if resp != nil {
					require.NoError(t, resp.Body.Close())
				}
				require.ErrorIs(t, err, ErrBlockedIP)
			})
		}
	}
}

func TestCatalogRebindingRejectedByClient(t *testing.T) {
	t.Parallel()
	option, err := WithInternalCatalogCIDR("10.23.45.67/32")
	require.NoError(t, err)
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.23.45.67")}, nil
	}})
	policy := NewDefaultPolicy(noop.NewTracerProvider(), option, WithResolver(resolver)) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
	endpoint := "https://alpha.catalog.dev.speakeasy.com/okta/mcp"
	_, err = policy.ValidateHTTPURL(t.Context(), endpoint, WithInternalCatalog())
	require.NoError(t, err)
	// The runtime lookup now returns loopback. It must fail before connecting,
	// despite preflight succeeding.
	policy.resolver = dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}})
	client := policy.PooledClient(WithInternalCatalog())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorIs(t, err, ErrBlockedIP)
}

// catalogTLSFixture permits one loopback address in place of the private ILB.
// The real Guardian client resolves and connects normally, including TLS.
func catalogTLSFixture(t *testing.T, trusted bool, handler http.Handler) (*http.Client, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"*.catalog.dev.speakeasy.com", "alternate.example.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	parsedCert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	if trusted {
		roots.AddCert(parsedCert)
	}
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "private.example.test" || host == "private.example.test." {
			return []net.IP{net.ParseIP("127.0.0.2")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}})
	policy := NewDefaultPolicy(noop.NewTracerProvider(), WithResolver(resolver), WithTLSRootCAs(roots)) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
	policy.internalCatalog = mustParseCIDR("127.0.0.1/32")
	client := policy.PooledClient(WithInternalCatalog())
	t.Cleanup(client.CloseIdleConnections)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)
	return client, port
}

func TestCatalogTransportAndRedirects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, targetHost string
		valid            bool
	}{
		{"same origin", "alpha.catalog.dev.speakeasy.com", true},
		{"second customer", "beta.catalog.dev.speakeasy.com", true},
		{"alternate hostname", "alternate.example.test", true},
		{"wrong certificate hostname", "wrong.example.test", false},
		{"other private host", "private.example.test", false},
		{"literal with valid certificate", "127.0.0.1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, port := catalogTLSFixture(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ProtoMajor != 2 {
					t.Errorf("expected HTTP/2, got %s", r.Proto)
				}
				if r.URL.Path == "/redirect" {
					_, port, err := net.SplitHostPort(r.Host)
					if err != nil {
						t.Errorf("split request host: %v", err)
						return
					}
					http.Redirect(w, r, "https://"+net.JoinHostPort(tc.targetHost, port)+"/okta/mcp", http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://alpha.catalog.dev.speakeasy.com:"+port+"/redirect", nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			if tc.valid {
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, http.StatusNoContent, resp.StatusCode)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCatalogTLSCertificateStillVerified(t *testing.T) {
	t.Parallel()
	client, port := catalogTLSFixture(t, false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS reached handler") }))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://alpha.catalog.dev.speakeasy.com:"+port+"/okta/mcp", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	var urlError *url.Error
	require.ErrorAs(t, err, &urlError)
	var unknownAuthority x509.UnknownAuthorityError
	require.ErrorAs(t, err, &unknownAuthority)
}
