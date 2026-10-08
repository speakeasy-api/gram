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
	"net/netip"
	"net/url"
	"strconv"
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
		{"lookalike", "https://alpha.catalog.dev.speakeasy.com.evil.test/mcp", []string{"10.23.45.67"}, false},
		{"missing boundary", "https://alphacatalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"unicode lookalike", "https://alpha.catalog.dev.speaKeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"apex", "https://catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"nested label", "https://nested.alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"invalid label", "https://-alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"literal", "https://10.23.45.67/mcp", nil, false},
		{"mapped literal", "https://[::ffff:10.23.45.67]/mcp", nil, false},
		{"http", "http://alpha.catalog.dev.speakeasy.com:443/mcp", []string{"10.23.45.67"}, false},
		{"other port", "https://alpha.catalog.dev.speakeasy.com:8443/mcp", []string{"10.23.45.67"}, false},
		{"empty port", "https://alpha.catalog.dev.speakeasy.com:/mcp", []string{"10.23.45.67"}, false},
		{"userinfo", "https://user@alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67"}, false},
		{"other private IP", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.68"}, false},
		{"public IP", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"8.8.8.8"}, false},
		{"mixed DNS", "https://alpha.catalog.dev.speakeasy.com/mcp", []string{"10.23.45.67", "8.8.8.8"}, false},
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

func TestCatalogSocketDestination(t *testing.T) {
	t.Parallel()
	option, err := WithInternalCatalogCIDR("10.23.45.67/32")
	require.NoError(t, err)
	policy := NewDefaultPolicy(noop.NewTracerProvider(), option) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
	for _, tc := range []struct {
		address string
		valid   bool
	}{
		{"10.23.45.67:443", true}, {"[::ffff:10.23.45.67]:443", true},
		{"10.23.45.68:443", false}, {"10.23.45.67:80", false},
		{"8.8.8.8:443", false}, {"[::1]:443", false}, {"bad", false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			t.Parallel()
			err := policy.internalCatalog.checkDestination(tc.address)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrBlockedIP)
			}
		})
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
	// despite preflight succeeding and even if another option allows loopback.
	policy.resolver = dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}})
	client := policy.PooledClient(WithInternalCatalog(), WithAllowedCIDRBlocks("127.0.0.1/32"))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorIs(t, err, ErrBlockedIP)
}

// catalogTLSFixture substitutes only the expected socket and port for a local
// TLS listener. Requests still use the catalog hostname, HTTPS/443 URL checks,
// DNS resolver, real Guardian ControlContext and normal certificate validation.
func catalogTLSFixture(t *testing.T, trusted bool, handler http.Handler) *http.Client {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"*.catalog.dev.speakeasy.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	parsedCert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	if trusted {
		roots.AddCert(parsedCert)
	}
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}})
	policy := NewDefaultPolicy(noop.NewTracerProvider(), WithResolver(resolver), WithTLSRootCAs(roots)) //nolint:forbidigo // testenv imports guardian through its identity fixtures, causing an import cycle.
	local := netip.MustParseAddrPort(server.Listener.Addr().String())
	policy.internalCatalog = &catalogRule{destination: local}
	base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	catalog := policy.catalogTransport(base, policy.Dialer())
	dial := catalog.DialContext
	catalog.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("split fixture address: %w", err)
		}
		return dial(ctx, network, net.JoinHostPort(host, strconv.Itoa(int(local.Port()))))
	}
	t.Cleanup(catalog.CloseIdleConnections)
	return &http.Client{Transport: &catalogRoundTripper{next: policy.Client().Transport, catalog: catalog}}
}

func TestCatalogTransportAndRedirects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, target string
		valid        bool
	}{
		{"same origin", "/okta/mcp", true},
		{"second customer", "https://beta.catalog.dev.speakeasy.com/okta/mcp", true},
		{"lookalike", "https://alpha.catalog.dev.speakeasy.com.evil.test/mcp", false},
		{"other private host", "https://private.example.test/mcp", false},
		{"literal", "https://10.23.45.67/mcp", false},
		{"scheme", "http://alpha.catalog.dev.speakeasy.com/okta/mcp", false},
		{"port", "https://alpha.catalog.dev.speakeasy.com:8443/okta/mcp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := catalogTLSFixture(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, tc.target, http.StatusFound)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://alpha.catalog.dev.speakeasy.com/redirect", nil)
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
	client := catalogTLSFixture(t, false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS reached handler") }))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://alpha.catalog.dev.speakeasy.com/okta/mcp", nil)
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
