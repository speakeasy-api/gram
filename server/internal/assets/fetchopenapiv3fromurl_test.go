package assets_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const fetchOpenAPIYAML = `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      summary: Test endpoint
      responses:
        '200':
          description: Success
`

func fetchOpenAPIForm(rawURL string) *gen.FetchOpenAPIv3FromURLForm {
	return &gen.FetchOpenAPIv3FromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              rawURL,
	}
}

func newTLSOpenAPIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func unsafeFetchPolicy(t *testing.T, blocklist []string, resolver dns.Resolver, servers ...*httptest.Server) *guardian.Policy {
	t.Helper()
	pool := x509.NewCertPool()
	for _, srv := range servers {
		pool.AddCert(srv.Certificate())
	}
	opts := []func(*guardian.Policy){guardian.WithTLSRootCAs(pool)}
	if resolver != nil {
		opts = append(opts, guardian.WithResolver(resolver))
	}
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), blocklist, opts...)
	require.NoError(t, err)
	return policy
}

func TestService_FetchOpenAPIv3FromURL_Success(t *testing.T) {
	t.Parallel()

	upstream := newTLSOpenAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(fetchOpenAPIYAML))
	})

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, upstream))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(upstream.URL+"/openapi.yaml"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "openapiv3", result.Asset.Kind)
	require.NotEmpty(t, result.Asset.Sha256)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_FetchOpenAPIv3FromURL_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)
	ctx := t.Context()

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm("https://example.com/openapi.yaml"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_FetchOpenAPIv3FromURL_RejectsNonHTTPSScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	for _, rawURL := range []string{
		"http://example.com/openapi.yaml",
		"file:///etc/passwd",
		"ftp://example.com/openapi.yaml",
		"gopher://example.com/1",
		"data:application/json,{}",
	} {
		_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(rawURL))
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, "url %s", rawURL)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, "url %s", rawURL)
		require.Equal(t, "invalid URL", oopsErr.Error(), "url %s", rawURL)
	}
}

func TestService_FetchOpenAPIv3FromURL_RejectsEmptyHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm("https:///openapi.yaml"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invalid URL", oopsErr.Error())
}

func TestService_FetchOpenAPIv3FromURL_RejectsBlockedIPLiterals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsServiceWithPolicy(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))

	for _, rawURL := range []string{
		"https://127.0.0.1/openapi.yaml",
		"https://10.0.0.1/openapi.yaml",
		"https://172.16.0.1/openapi.yaml",
		"https://192.168.1.1/openapi.yaml",
		"https://169.254.169.254/latest/meta-data/",
		"https://[::1]/openapi.yaml",
	} {
		_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(rawURL))
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr, "url %s", rawURL)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code, "url %s", rawURL)
		require.Equal(t, "invalid URL", oopsErr.Error(), "url %s", rawURL)
	}
}

func TestService_FetchOpenAPIv3FromURL_RejectsBlockedHostname(t *testing.T) {
	t.Parallel()

	const blockedHost = "internal.test"
	policy := guardian.NewDefaultPolicy(
		testenv.NewTracerProvider(t),
		guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{
			LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
				if host == blockedHost {
					return []net.IP{net.ParseIP("10.0.0.1")}, nil
				}
				return nil, fmt.Errorf("unexpected host: %s", host)
			},
		})),
	)

	ctx, ti := newTestAssetsServiceWithPolicy(t, policy)

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm("https://"+blockedHost+"/openapi.yaml"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invalid URL", oopsErr.Error())
}

func TestService_FetchOpenAPIv3FromURL_RedirectToBlockedHost(t *testing.T) {
	t.Parallel()

	const blockedHost = "blocked.test"
	blockedIP := net.ParseIP("203.0.113.1") // RFC 5737 TEST-NET-3

	mockResolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			if host == blockedHost {
				return []net.IP{blockedIP}, nil
			}
			return nil, fmt.Errorf("unexpected host: %s", host)
		},
	})

	// Block TEST-NET-3 only; loopback stays reachable so the httptest
	// TLS server (which listens on 127.0.0.1) can serve the initial request.
	upstream := newTLSOpenAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://"+blockedHost+"/openapi.yaml")
		w.WriteHeader(http.StatusFound)
	})
	policy := unsafeFetchPolicy(t, []string{"203.0.113.0/24"}, mockResolver, upstream)

	ctx, ti := newTestAssetsServiceWithPolicy(t, policy)

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(upstream.URL))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "host is not allowed", oopsErr.Error())
	require.ErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestService_FetchOpenAPIv3FromURL_FollowsAllowedRedirect(t *testing.T) {
	t.Parallel()

	dest := newTLSOpenAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(fetchOpenAPIYAML))
	})
	src := newTLSOpenAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL+"/openapi.yaml", http.StatusFound)
	})

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, src, dest))

	result, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(src.URL))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.Equal(t, "openapiv3", result.Asset.Kind)
}

func TestService_FetchOpenAPIv3FromURL_RedirectToUnresolvableHost(t *testing.T) {
	t.Parallel()

	const brokenHost = "broken.test"
	mockResolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			if host == brokenHost {
				return nil, fmt.Errorf("mock resolver: nxdomain")
			}
			return nil, fmt.Errorf("unexpected host: %s", host)
		},
	})

	upstream := newTLSOpenAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://"+brokenHost+"/openapi.yaml")
		w.WriteHeader(http.StatusFound)
	})
	policy := unsafeFetchPolicy(t, []string{}, mockResolver, upstream)

	ctx, ti := newTestAssetsServiceWithPolicy(t, policy)

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(upstream.URL))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "error fetching URL", oopsErr.Error())
	require.ErrorIs(t, err, guardian.ErrBadHost)
	require.NotErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestService_FetchOpenAPIv3FromURL_RedirectsCapped(t *testing.T) {
	t.Parallel()

	upstream := newTLSOpenAPIServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", r.URL.Path+"/next")
		w.WriteHeader(http.StatusFound)
	})

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, upstream))

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(upstream.URL+"/start"))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "error fetching URL", oopsErr.Error())
	require.Contains(t, oopsErr.String(), "stopped after 3 redirects")
}

func TestService_FetchOpenAPIv3FromURL_RejectsHTTPRedirect(t *testing.T) {
	t.Parallel()

	upstream := newTLSOpenAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://8.8.8.8/openapi.yaml")
		w.WriteHeader(http.StatusFound)
	})

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, upstream))

	_, err := ti.service.FetchOpenAPIv3FromURL(ctx, fetchOpenAPIForm(upstream.URL))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "error fetching URL", oopsErr.Error())
	require.NotErrorIs(t, err, guardian.ErrBlockedIP)
}

func TestService_FetchImageAssetFromURL_RejectsHTTPScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.FetchImageAssetFromURL(ctx, "http://example.com/favicon.ico")

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invalid URL", oopsErr.Error())
}

func TestService_FetchImageAssetFromURL_RejectsBlockedIPLiteral(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsServiceWithPolicy(t, guardian.NewDefaultPolicy(testenv.NewTracerProvider(t)))

	_, err := ti.service.FetchImageAssetFromURL(ctx, "https://169.254.169.254/latest/meta-data/")

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Equal(t, "invalid URL", oopsErr.Error())
}
