package remotemcp_test

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcpproxy "github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newPermissivePolicy(t *testing.T) *guardian.Policy {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return policy
}

func TestProbeURL_RBACForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://mcp.example.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestProbeURL_InvalidURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "ftp://example.com",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestProbeURL_RejectsHostedHTTP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "http://8.8.8.8/mcp",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorIs(t, err, remotemcpproxy.ErrInsecureRemoteMCPTransport)
}

func TestProbeURL_RejectsUserinfo(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://user:secret@mcp.example.com/mcp",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorIs(t, err, remotemcpproxy.ErrRemoteMCPURLUserinfo)
}

func TestProbeURL_BlockedHostIsStructuredUnreachable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	result, err := ti.service.ProbeURL(ctx, &gen.ProbeURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: "https://" + blockedTestHost,
	})
	require.NoError(t, err)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonGuardianRejected, requireValue(t, result.Reason))
	require.Nil(t, result.HTTPStatus)
}

func TestVerifyURL_CompatibilityInvalidMCPResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	}))
	t.Cleanup(upstream.Close)

	ctx, ti := newTestServiceWithPolicy(t, newPermissivePolicy(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: upstream.URL, TransportType: "streamable-http",
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
	require.Equal(t, "Reachable: although received unexpected MCP response", result.Message)
}

func TestVerifyURL_CompatibilityAuthenticationRequired(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	ctx, ti := newTestServiceWithPolicy(t, newPermissivePolicy(t))
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})
	result, err := ti.service.VerifyURL(ctx, &gen.VerifyURLPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, URL: upstream.URL, TransportType: "streamable-http",
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, http.StatusUnauthorized, requireValue(t, result.HTTPStatus))
	require.Equal(t, "Reachable: received authorization required response", result.Message)
}

func TestProbeRemoteMcpURL_AcceptsInitializeJSONRPCResponse(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Accept"), "text/event-stream")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
	require.Nil(t, result.HTTPStatus)
}

func TestProbeRemoteMcpURL_AcceptsCorrelatedJSONRPCError(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"unsupported"}}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_RejectsNonResponseJSONRPC(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
}

func TestProbeRemoteMcpURL_RejectsMismatchedResponseID(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
}

func TestProbeRemoteMcpURL_RejectsAmbiguousJSONRPC(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":2,"id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
}

func TestProbeRemoteMcpURL_AcceptsCorrelatedSSEEventWithoutWaitingForEOF(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"))
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(releaseHandler)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	releaseHandler()
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
}

func TestProbeRemoteMcpURL_RejectsNonJSONRPCSSE(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: server is healthy\n\n"))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
}

func TestProbeRemoteMcpURL_Truncated2xxIsInvalidMCP(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "128")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, result.Outcome)
	require.Equal(t, http.StatusOK, requireValue(t, result.HTTPStatus))
	require.Nil(t, result.Reason)
}

func TestProbeRemoteMcpURL_AuthenticationRequiredWithMetadata(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="https://auth.example.com/.well-known/oauth-protected-resource/mcp"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, result.Outcome)
	require.Equal(t, "https://auth.example.com/.well-known/oauth-protected-resource/mcp", requireValue(t, result.ProtectedResourceMetadataURL))
	require.Nil(t, result.HTTPStatus)
}

func TestProbeRemoteMcpURL_IgnoresInvalidAuthenticationMetadata(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("WWW-Authenticate", `Bearer resource_metadata="/relative"`)
		w.Header().Add("WWW-Authenticate", `Bearer resource_metadata="https://user:secret@auth.example.com/metadata"`)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeAuthenticationRequired, result.Outcome)
	require.Nil(t, result.ProtectedResourceMetadataURL)
}

func TestProbeRemoteMcpURL_ClassifiesHTTPFailures(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/timeout":
			w.WriteHeader(http.StatusRequestTimeout)
		case "/rate-limit":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/server-error":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	policy := newPermissivePolicy(t)

	timeout := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/timeout")
	rateLimit := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/rate-limit")
	serverError := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/server-error")
	notFound := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL+"/not-found")
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, timeout.Reason))
	require.Equal(t, remotemcp.ProbeReasonRateLimited, requireValue(t, rateLimit.Reason))
	require.Equal(t, remotemcp.ProbeReasonServerError, requireValue(t, serverError.Reason))
	require.Equal(t, remotemcp.ProbeOutcomeInvalidMCPResponse, notFound.Outcome)
}

func TestProbeRemoteMcpURL_ResponseBodyTimeoutDoesNotHang(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(releaseHandler)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	result := remotemcp.ProbeRemoteMcpURL(ctx, newPermissivePolicy(t), upstream.URL)
	releaseHandler()
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTimeout, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_DNSFailure(t *testing.T) {
	t.Parallel()
	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithResolver(resolver))
	require.NoError(t, err)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, "https://unresolvable.test/mcp")
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonDNSError, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_TLSFailure(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTLSError, requireValue(t, result.Reason))
}

func TestProbeRemoteMcpURL_PreservesInitializeAcrossRedirects(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, string(body), `"method":"initialize"`)
		if r.URL.Path != "/3" {
			w.Header().Set("Location", fmt.Sprintf("/%d", requests.Load()))
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL+"/0")
	require.Equal(t, remotemcp.ProbeOutcomeMCPAvailable, result.Outcome)
	require.Equal(t, int32(4), requests.Load())
}

func TestProbeRemoteMcpURL_RejectsFourthRedirect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Location", fmt.Sprintf("/%d", requests.Load()))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), newPermissivePolicy(t), upstream.URL+"/0")
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTransportError, requireValue(t, result.Reason))
	require.Equal(t, int32(4), requests.Load())
}

func TestProbeRemoteMcpURL_RejectsHTTPSRedirectToHostedHTTP(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://8.8.8.8/mcp")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonTransportError, requireValue(t, result.Reason))
	require.Nil(t, result.HTTPStatus)
}

func TestProbeRemoteMcpURL_RejectsRedirectToBlockedHost(t *testing.T) {
	t.Parallel()
	const blockedHost = "blocked.test"
	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(_ context.Context, _, host string) ([]net.IP, error) {
			if host == blockedHost {
				return []net.IP{net.ParseIP("203.0.113.1")}, nil
			}
			return nil, fmt.Errorf("unexpected host: %s", host)
		},
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"203.0.113.0/24"}, guardian.WithResolver(resolver))
	require.NoError(t, err)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://"+blockedHost+"/initialize")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(upstream.Close)

	result := remotemcp.ProbeRemoteMcpURL(t.Context(), policy, upstream.URL)
	require.Equal(t, remotemcp.ProbeOutcomeUnreachable, result.Outcome)
	require.Equal(t, remotemcp.ProbeReasonGuardianRejected, requireValue(t, result.Reason))
}

func requireValue[T any](t *testing.T, value *T) T {
	t.Helper()
	require.NotNil(t, value)
	return *value
}
