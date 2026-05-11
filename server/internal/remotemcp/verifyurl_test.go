package remotemcp_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newPermissivePolicy returns a guardian.Policy that allows loopback so
// httptest.NewServer-backed probes can dial 127.0.0.1.
func newPermissivePolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

func TestVerifyURL_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestVerifyURL_Unauthorized_NoProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestVerifyURL_InvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "ftp://example.com",
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestVerifyURL_BlockedHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "http://" + blockedTestHost,
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestVerifyRemoteMcpURL_OkJSONRPC(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Accept"), "application/json")
		assert.Contains(t, r.Header.Get("Accept"), "text/event-stream")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.True(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusOK, *status)
	require.Equal(t, "Success", message)
}

func TestVerifyRemoteMcpURL_OkSSE(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.True(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusOK, *status)
	require.Equal(t, "Success", message)
}

func TestVerifyRemoteMcpURL_Unauthorized(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.True(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusUnauthorized, *status)
	require.Equal(t, "Reachable: received authorization required response", message)
}

func TestVerifyRemoteMcpURL_Forbidden(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.True(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusForbidden, *status)
	require.Equal(t, "Reachable: received authorization required response", message)
}

func TestVerifyRemoteMcpURL_HTMLNotMCP(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>hello</html>"))
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.True(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusOK, *status)
	require.Equal(t, "Reachable: although received unexpected MCP response", message)
}

func TestVerifyRemoteMcpURL_NotFound(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.False(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusNotFound, *status)
	require.Equal(t, "MCP response not found", message)
}

func TestVerifyRemoteMcpURL_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.False(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusMethodNotAllowed, *status)
	require.Equal(t, "Unexpected response from server", message)
}

func TestVerifyRemoteMcpURL_ServerError(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.False(t, verified)
	require.NotNil(t, status)
	require.Equal(t, http.StatusInternalServerError, *status)
	require.Equal(t, "Unexpected response from server", message)
}

// TestVerifyRemoteMcpURL_RedirectToBlockedHost exercises the dial-layer SSRF
// guard. CheckRedirect only bounds redirect count; the safety net for a
// hostile target redirecting into a private range is the policy.Client
// dialer's ControlContext, which rejects after DNS resolution. The mock
// resolver maps blockedHost into the policy's blocked CIDR so the redirect
// dial trips ErrBlockedIP rather than reaching anything internal.
func TestVerifyRemoteMcpURL_RedirectToBlockedHost(t *testing.T) {
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
	// server (which listens on 127.0.0.1) can serve the initial request.
	policy, err := guardian.NewUnsafePolicy(
		testenv.NewTracerProvider(t),
		[]string{"203.0.113.0/24"},
		guardian.WithResolver(mockResolver),
	)
	require.NoError(t, err)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://"+blockedHost+"/initialize")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	verified, status, message := remotemcp.VerifyRemoteMcpURL(t.Context(), policy, upstream.URL)

	require.False(t, verified)
	require.Nil(t, status, "transport-layer rejection should not surface an HTTP status")
	require.Equal(t, "Host is not allowed", message)
}
