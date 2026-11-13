package remotemcp

import (
	"context"
	"errors"
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
	require.Equal(t, "remote mcp server address is not allowed by network policy", shared.Error())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeAddressNotAllowed)
	require.Empty(t, row.ProviderHost)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}

func TestToolsCallConnections_HostThatDoesNotExist(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	notFound := &net.DNSError{Err: "no such host", Name: "mcp.example.invalid", IsNotFound: true}
	shared := postToolsCallThrough(t, recordingConnections(rec, "token"), resolvingPolicy(t, notFound), "http://mcp.example.invalid/mcp")

	require.Equal(t, oops.CodeGatewayError, shared.Code)
	require.Equal(t, "remote mcp server address could not be found", shared.Error())
	row := rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, mcpconnectionattempts.CodeAddressNotFound)
	require.Empty(t, row.ProviderHost)
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
