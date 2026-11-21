package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// postToolsCallThrough sends a tools/call through a direct server's proxy built on policy and returns the proxy's error.
func postToolsCallThrough(t *testing.T, connections connectionsUnderTest, policy *guardian.Policy, upstreamURL string) *oops.ShareableError {
	t.Helper()

	p := newKillswitchTestProxy(t, upstreamURL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
	p.GuardianPolicy = policy
	connections.install(p)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	err := p.Post(httptest.NewRecorder(), req)
	shared, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "proxy error: %v", err)
	return shared
}

// resolvingPolicy is a policy whose lookups all return lookupErr.
func resolvingPolicy(t *testing.T, lookupErr error) *guardian.Policy {
	t.Helper()

	resolver := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return nil, lookupErr },
	})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithResolver(resolver))
	require.NoError(t, err)
	return policy
}

func TestToolsCallConnections_BlockedAddressIsNotTheProviders(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a blocked address must not be dialed")
	}))
	t.Cleanup(upstream.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"127.0.0.0/8", "::1/128"})
	require.NoError(t, err)

	rec := mcpconnectionattemptstest.New(t)
	shared := postToolsCallThrough(t, recordingConnections(rec, "token"), policy, upstream.URL)

	require.Equal(t, oops.CodeGatewayError, shared.Code)
	require.Equal(t, "This server can't connect: its address is a private or internal one that Speakeasy can't reach. Waiting won't fix this. Ask your administrator to fix the server's address.", shared.Error())
	require.NotRegexp(t, `(?i)provider|credential|token|url|tunnel`, shared.Error())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeAddressNotAllowed)
	require.Equal(t, "provider.example.com", row.ProviderHost, "the configured host, never the address it resolved to")
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

func TestToolsCallConnections_HostThatDoesNotExist(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	notFound := &net.DNSError{Err: "no such host", Name: "mcp.example.invalid", IsNotFound: true}
	shared := postToolsCallThrough(t, recordingConnections(rec, "token"), resolvingPolicy(t, notFound), "http://mcp.example.invalid/mcp")

	require.Equal(t, oops.CodeGatewayError, shared.Code)
	require.Equal(t, "This server can't connect: its address doesn't exist. Waiting won't fix this. Ask your administrator to fix the server's address.", shared.Error())
	require.NotRegexp(t, `(?i)provider|credential|token|url|tunnel`, shared.Error())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeAddressNotFound)
	require.Equal(t, "provider.example.com", row.ProviderHost)
}

func TestToolsCallConnections_TemporaryLookupFailureStaysUnreachable(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	shared := postToolsCallThrough(t, recordingConnections(rec, "token"), resolvingPolicy(t, errors.New("resolver down")), "http://mcp.example.invalid/mcp")

	require.Equal(t, oops.CodeGatewayError, shared.Code)
	require.Equal(t, "remote mcp server unreachable", shared.Error())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnreachable)
	require.Equal(t, "provider.example.com", row.ProviderHost)
}

// An upstream that redirects to a refused address chose that address; the configured one is fine.
func TestToolsCallConnections_RedirectToBlockedAddressKeepsItsReading(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.10/mcp", http.StatusFound)
	}))
	t.Cleanup(upstream.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithBlockedCIDRBlocks("192.0.2.0/24"))
	require.NoError(t, err)

	rec := mcpconnectionattemptstest.New(t)
	connections := recordingConnections(rec, "token")
	p := newKillswitchTestProxy(t, upstream.URL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
	p.GuardianPolicy = policy
	connections.install(p)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp/server", strings.NewReader(connectionToolsCall))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	err = p.Post(httptest.NewRecorder(), req)
	shared, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, "proxy error: %v", err)

	require.Equal(t, "remote mcp server unreachable", shared.Error())
	rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeProviderUnreachable)
}

// A tunnel's dial reaches our own gateway, so a refused address there is ours, never the admin's.
func TestUpstreamFailureHappened_TunnelDialIsNeverAnAddress(t *testing.T) {
	t.Parallel()

	refused := fmt.Errorf("dial gateway: %w", guardian.ErrBlockedIP)
	code, ok := UpstreamFailureHappened(0, false, UpstreamCredentialSent{Attached: true, Self: false, Configured: false}, true, "", refused)
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code)
}

// A call the phase timer gave up on stays a timeout even when a refused address is also in its chain.
func TestUpstreamFailureHappened_TimeoutWinsOverAddress(t *testing.T) {
	t.Parallel()

	code, ok := UpstreamFailureHappened(0, false, UpstreamCredentialSent{Attached: true, Self: false, Configured: false}, false, "",
		fmt.Errorf("forward: %w", proxy.ErrUpstreamTimedOut), fmt.Errorf("dial: %w", guardian.ErrBlockedIP))
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code)
}

// coldCatalogRefusal sends a tools/call whose tool the catalog has not seen, so the server is listed first through
// policy and, when set, respond.
func coldCatalogRefusal(t *testing.T, policy *guardian.Policy, upstreamURL string, respond func(context.Context, *http.Response) error) (mcpconnectionattempts.Code, error) {
	t.Helper()

	p := newKillswitchTestProxy(t, upstreamURL, NewToolsCallKillswitchInterceptor(continueCheckpoint(), "organization-id", "server-id", testenv.NewLogger(t)))
	p.GuardianPolicy = policy
	p.UpstreamResponseInterceptor = respond
	store := &fakeToolCatalogStore{catalog: map[string]string{}}
	err := newAuthzInterceptor(t, store, newCatalogSync(store, p.ListUpstreamTools)).InterceptToolsCallRequest(blockDestructiveContext(t), newCatalogCall(t, "delete_repository"))
	require.Error(t, err)
	code, ok := refusalHappened(err, false)
	require.True(t, ok)
	return code, err
}

func TestToolsCallConnections_ColdCatalogAtBlockedAddress(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a blocked address must not be dialed")
	}))
	t.Cleanup(upstream.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{"127.0.0.0/8", "::1/128"})
	require.NoError(t, err)

	code, _ := coldCatalogRefusal(t, policy, upstream.URL, nil)
	require.Equal(t, mcpconnectionattempts.CodeAddressNotAllowed, code)

	tunneledCode, _ := refusalHappened(fmt.Errorf("list: %w", guardian.ErrBlockedIP), true)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, tunneledCode, "a tunnel dial is ours")
}

func TestToolsCallConnections_ColdCatalogAtMissingHost(t *testing.T) {
	t.Parallel()

	code, _ := coldCatalogRefusal(t, resolvingPolicy(t, &net.DNSError{Err: "no such host", Name: "mcp.example.invalid", IsNotFound: true}), "http://mcp.example.invalid/mcp", nil)
	require.Equal(t, mcpconnectionattempts.CodeAddressNotFound, code)
}

// A listing the server never answered reads like a forwarded call failing the same way; one it answered stays ours.
func TestToolsCallConnections_ColdCatalogUnansweredListingIsUnreachable(t *testing.T) {
	t.Parallel()

	code, _ := coldCatalogRefusal(t, resolvingPolicy(t, errors.New("resolver down")), "http://mcp.example.invalid/mcp", nil)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code, "a lookup that fails for now")

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	open, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	code, _ = coldCatalogRefusal(t, open, closedURL, nil)
	require.Equal(t, mcpconnectionattempts.CodeProviderUnreachable, code, "a server that refused the dial")

	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(refused.Close)
	code, _ = coldCatalogRefusal(t, open, refused.URL, nil)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "a server that answered")

	answered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(answered.Close)
	code, err = coldCatalogRefusal(t, open, answered.URL, func(context.Context, *http.Response) error {
		return oops.E(oops.CodeUnauthorized, nil, "credential refused")
	})
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "a response check that refused an answer")
	listing, ok := errors.AsType[*proxy.UpstreamListingRefusalError](err)
	require.True(t, ok, "listing error: %v", err)
	require.True(t, listing.Answered)
}
