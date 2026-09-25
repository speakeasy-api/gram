package mcp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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
	proxyManager := remotemcp.NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	project, tunnel, wrapper, meta := uuid.New(), uuid.New(), uuid.New(), uuid.NewString()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", ProjectID: &project})
	ctx = mcpidentity.NewValidatorBoundary().StampAgent(ctx, uuid.New())
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(ctx, tunnel.String(), "http://gateway.example", time.Hour))
	manager := newTunnelManager(routes, "", proxyManager, nil, issuer)
	server := &mcpserversrepo.McpServer{ID: wrapper, TunneledMcpServerID: conv.ToNullUUID(tunnel), Visibility: mcpservers.VisibilityPrivate}
	p, err := manager.buildProxy(ctx, "", logger, project, "org_test", server, "", "upstream-oauth", "", nil, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	require.NotNil(t, p.CallerAssertion)
	raw, err := p.CallerAssertion(ctx)
	require.NoError(t, err)
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(urn.NewTunneledMcpServer(tunnel).String()), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, wrapper.String(), claims["mcp_server_id"])
	require.Equal(t, "agent", claims["principal_type"])
	require.NotContains(t, claims, "user_id")
	require.NotEqual(t, meta, claims["aud"])
	resource := "https://mcp.internal.example.com/a%2Fb/?tenant=example/"
	configured, err := manager.buildProxy(ctx, "", logger, project, "org_test", server, resource, "upstream-oauth", "", nil, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	raw, err = configured.CallerAssertion(ctx)
	require.NoError(t, err)
	token, err = jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(resource), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok = token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, resource, claims["aud"])
	require.Equal(t, tunnel.String(), claims["tunneled_mcp_server_id"])
	server.Visibility = mcpservers.VisibilityPublic
	publicProxy, err := manager.buildProxy(ctx, "", logger, project, "org_test", server, "", "", "", nil)
	require.NoError(t, err)
	require.Nil(t, publicProxy.CallerAssertion)
	// Pinned public sessions use BuildTarget directly.
	pinned := proxyManager.BuildTarget(logger, proxy.ServerIdentity{TunneledMCPServerID: tunnel.String(), McpServerID: wrapper.String()}, "http://gateway.example", nil, mcpservers.VisibilityPublic, "org_test", project.String(), "", "", nil)
	require.Nil(t, pinned.CallerAssertion)
	remoteProxy := proxyManager.BuildTarget(logger, proxy.ServerIdentity{RemoteMCPServerID: uuid.NewString(), McpServerID: wrapper.String()}, "https://remote.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "", "", nil)
	require.Nil(t, remoteProxy.CallerAssertion)
	manager.callerAssertions = nil
	server.Visibility = mcpservers.VisibilityPrivate
	disabled, err := manager.buildProxy(ctx, "", logger, project, "org_test", server, "", "", "", nil)
	require.NoError(t, err)
	require.Nil(t, disabled.CallerAssertion)
}
