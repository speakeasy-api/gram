package mcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/metrics"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/stretchr/testify/require"
)

func TestTunnelManagerCallerAssertionScopeAndMetaDestination(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	issuer, err := mcpauthz.New(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})), "https://gram.example", false)
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	proxyManager := remotemcp.NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	project, tunnel, wrapper, meta := uuid.New(), uuid.New(), uuid.New(), uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", ProjectID: &project})
	ctx = mcpidentity.NewValidatorBoundary().StampAgent(ctx, uuid.New())
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, tunnel.String(), "http://gateway.example", time.Hour))
	manager := newTunnelManager(routes, "", proxyManager, nil, issuer)
	server := &mcpserversrepo.McpServer{ID: wrapper, TunneledMcpServerID: conv.ToNullUUID(tunnel), Visibility: mcpservers.VisibilityPrivate}
	p, err := manager.buildProxy(ctx, logger, buildProxyParams{
		ClientAffinityKey:  "",
		ProjectID:          project,
		OrganizationID:     "org_test",
		MCPServer:          server,
		ResourceIdentifier: "",
		UpstreamAuth:       "upstream-oauth",
		WWWAuthenticate:    "",
		Selection:          nil,
		Request:            nil,
	}, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	require.NotNil(t, p.CallerAssertion)
	raw, err := p.CallerAssertion(ctx)
	require.NoError(t, err)
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(urn.NewTunneledMcpServer(tunnel).String()), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Regexp(t, "^agent:", claims["sub"])
	require.NotContains(t, claims, "user_id")
	require.NotEqual(t, meta, claims["aud"])
	resource := "https://mcp.internal.example.com/a%2Fb/?tenant=example/"
	configured, err := manager.buildProxy(ctx, logger, buildProxyParams{
		ClientAffinityKey:  "",
		ProjectID:          project,
		OrganizationID:     "org_test",
		MCPServer:          server,
		ResourceIdentifier: resource,
		UpstreamAuth:       "upstream-oauth",
		WWWAuthenticate:    "",
		Selection:          nil,
		Request:            nil,
	}, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	raw, err = configured.CallerAssertion(ctx)
	require.NoError(t, err)
	token, err = jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(resource), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok = token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, resource, claims["aud"])
	server.Visibility = mcpservers.VisibilityPublic
	publicProxy, err := manager.buildProxy(ctx, logger, buildProxyParams{
		ClientAffinityKey:  "",
		ProjectID:          project,
		OrganizationID:     "org_test",
		MCPServer:          server,
		ResourceIdentifier: "",
		UpstreamAuth:       "",
		WWWAuthenticate:    "",
		Selection:          nil,
		Request:            nil,
	})
	require.NoError(t, err)
	require.Nil(t, publicProxy.CallerAssertion)
	// Pinned public sessions use BuildTarget directly.
	pinned := proxyManager.BuildTarget(logger, proxy.ServerIdentity{TunneledMCPServerID: tunnel.String(), McpServerID: wrapper.String()}, "http://gateway.example", nil, mcpservers.VisibilityPublic, "org_test", project.String(), "", "", nil)
	require.Nil(t, pinned.CallerAssertion)
	remoteProxy := proxyManager.BuildTarget(logger, proxy.ServerIdentity{RemoteMCPServerID: uuid.NewString(), McpServerID: wrapper.String()}, "https://remote.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "", "", nil)
	require.Nil(t, remoteProxy.CallerAssertion)
}

func TestTunnelManagerRecordsRequestsWithNoConnectedAgent(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	proxyManager := remotemcp.NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	proxyManager.TunnelMetrics = metrics.New()
	project, tunnel, wrapper := uuid.New(), uuid.New(), uuid.New()
	manager := newTunnelManager(route.NewRouteTable(), "", proxyManager, nil, nil)
	server := &mcpserversrepo.McpServer{ID: wrapper, TunneledMcpServerID: conv.ToNullUUID(tunnel), Visibility: mcpservers.VisibilityPublic}
	build := func(body string) error {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("User-Agent", "Claude/1.0")
		_, err := manager.buildProxy(t.Context(), logger, buildProxyParams{
			ClientAffinityKey:  "",
			ProjectID:          project,
			OrganizationID:     "org_test",
			MCPServer:          server,
			ResourceIdentifier: "",
			UpstreamAuth:       "",
			WWWAuthenticate:    "",
			Selection:          nil,
			Request:            req,
		})
		return err
	}
	require.Error(t, build(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"PRIVATE_TOOL"}}`))
	// Notifications and batches are not requests the proxy would observe either.
	require.Error(t, build(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	require.Error(t, build(`[{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`))

	var rows []metrics.Snapshot
	proxyManager.TunnelMetrics.Flush(t.Context(), func(_ context.Context, s metrics.Snapshot) error {
		if s.Kind == "requests" {
			rows = append(rows, s)
		}
		return nil
	})
	require.Len(t, rows, 1)
	require.Equal(t, tunnel.String(), rows[0].SourceID)
	require.Equal(t, wrapper.String(), rows[0].ServerID)
	require.Equal(t, "tools/call", rows[0].Method)
	require.Equal(t, "claude", rows[0].ClientFamily)
	require.EqualValues(t, 1, rows[0].Attempts)
	require.EqualValues(t, 1, rows[0].Errors)
	require.Equal(t, [12]uint64{}, rows[0].LatencyBins, "an outage must not pull response times toward zero")
}
