package remotemcp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestProxyManagerCallerAssertionScopeAndMetaDestination(t *testing.T) {
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
	manager := NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, issuer)
	project, tunnel, wrapper, meta := uuid.New(), uuid.New(), uuid.NewString(), uuid.NewString()
	identity := proxy.ServerIdentity{TunneledMCPServerID: tunnel.String(), McpServerID: wrapper}
	p := manager.BuildTarget(logger, identity, "http://gateway.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "upstream-oauth", "", nil, WithMetaMCPServerID(meta))
	require.NotNil(t, p.CallerAssertion)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", ProjectID: &project})
	ctx = mcpidentity.NewValidatorBoundary().StampAgent(ctx, uuid.New())
	raw, err := p.CallerAssertion(ctx)
	require.NoError(t, err)
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(urn.NewTunneledMcpServer(tunnel).String()), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, wrapper, claims["mcp_server_id"])
	require.Equal(t, "agent", claims["principal_type"])
	require.NotContains(t, claims, "user_id")
	require.NotEqual(t, meta, claims["aud"])
	// This is also the direct BuildTarget path used by a pinned public session.
	publicProxy := manager.BuildTarget(logger, identity, "http://gateway.example", nil, mcpservers.VisibilityPublic, "org_test", project.String(), "", "", nil)
	require.Nil(t, publicProxy.CallerAssertion)
	remoteProxy := manager.BuildTarget(logger, proxy.ServerIdentity{RemoteMCPServerID: uuid.NewString(), McpServerID: wrapper}, "https://remote.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "", "", nil)
	require.Nil(t, remoteProxy.CallerAssertion)
	manager.callerAssertions = nil
	disabled := manager.BuildTarget(logger, identity, "http://gateway.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "", "", nil)
	require.Nil(t, disabled.CallerAssertion)
}
