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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/identity"
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
		Upstream:           upstreamBearer{Token: "upstream-oauth", Credential: nil},
		WWWAuthenticate:    "",
		Selection:          nil,
	}, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	require.NotNil(t, p.CallerAssertion)
	raw, err := p.CallerAssertion(ctx, nil)
	require.NoError(t, err)
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(urn.NewTunneledMcpServer(tunnel).String()), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Regexp(t, "^agent:", claims["sub"])
	require.NotContains(t, claims, "user_id")
	require.NotEqual(t, meta, claims["aud"])
	require.Equal(t, wrapper.String(), claims[identity.ClaimMCPServerID])
	require.NotContains(t, claims, identity.ClaimUpstreamCredential)
	cred := &identity.UpstreamCredential{Owner: identity.OwnerSelf, ClientID: uuid.NewString(), GrantID: "", GrantGeneration: 0, TokenSHA256: identity.TokenSHA256("upstream-oauth"), TokenExpiresAt: nil}
	raw, err = p.CallerAssertion(ctx, cred)
	require.NoError(t, err)
	token, err = jwt.Parse(raw, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithAudience(urn.NewTunneledMcpServer(tunnel).String()), jwt.WithValidMethods([]string{"RS256"}))
	require.NoError(t, err)
	claims, ok = token.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.Equal(t, map[string]any{"owner": cred.Owner, "client_id": cred.ClientID, "token_sha256": cred.TokenSHA256}, claims[identity.ClaimUpstreamCredential])
	resource := "https://mcp.internal.example.com/a%2Fb/?tenant=example/"
	configured, err := manager.buildProxy(ctx, logger, buildProxyParams{
		ClientAffinityKey:  "",
		ProjectID:          project,
		OrganizationID:     "org_test",
		MCPServer:          server,
		ResourceIdentifier: resource,
		Upstream:           upstreamBearer{Token: "upstream-oauth", Credential: nil},
		WWWAuthenticate:    "",
		Selection:          nil,
	}, remotemcp.WithMetaMCPServerID(meta))
	require.NoError(t, err)
	raw, err = configured.CallerAssertion(ctx, nil)
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
		Upstream:           upstreamBearer{Token: "", Credential: nil},
		WWWAuthenticate:    "",
		Selection:          nil,
	})
	require.NoError(t, err)
	require.Nil(t, publicProxy.CallerAssertion)
	// Pinned public sessions use BuildTarget directly.
	pinned := proxyManager.BuildTarget(logger, proxy.ServerIdentity{TunneledMCPServerID: tunnel.String(), McpServerID: wrapper.String()}, "http://gateway.example", nil, mcpservers.VisibilityPublic, "org_test", project.String(), "", "", nil)
	require.Nil(t, pinned.CallerAssertion)
	remoteProxy := proxyManager.BuildTarget(logger, proxy.ServerIdentity{RemoteMCPServerID: uuid.NewString(), McpServerID: wrapper.String()}, "https://remote.example", nil, mcpservers.VisibilityPrivate, "org_test", project.String(), "", "", nil)
	require.Nil(t, remoteProxy.CallerAssertion)
}

func TestTunnelManagerSetsBearerWithItsCredential(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	proxyManager := remotemcp.NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	tunnel := uuid.New()
	routes := route.NewRouteTable()
	require.NoError(t, routes.Publish(t.Context(), tunnel.String(), "http://gateway.example", time.Hour))
	manager := newTunnelManager(routes, "", proxyManager, nil, nil)
	server := &mcpserversrepo.McpServer{ID: uuid.New(), TunneledMcpServerID: conv.ToNullUUID(tunnel), Visibility: mcpservers.VisibilityPrivate}
	cred := &identity.UpstreamCredential{Owner: identity.OwnerSelf, ClientID: uuid.NewString(), GrantID: "", GrantGeneration: 0, TokenSHA256: identity.TokenSHA256("synthetic-token"), TokenExpiresAt: nil}
	p, err := manager.buildProxy(t.Context(), logger, buildProxyParams{
		ClientAffinityKey:  "",
		ProjectID:          uuid.New(),
		OrganizationID:     "org_test",
		MCPServer:          server,
		ResourceIdentifier: "",
		Upstream:           upstreamBearer{Token: "synthetic-token", Credential: cred},
		WWWAuthenticate:    "",
		Selection:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "synthetic-token", p.AuthorizationOverride)
	require.Same(t, cred, p.UpstreamCredential)
}

func TestUpstreamProvenance(t *testing.T) {
	t.Parallel()
	clientID, grantID := uuid.New(), uuid.New()
	expiresAt := time.Unix(1_900_000_000, 0)
	hash := identity.TokenSHA256("synthetic-token")
	subject := remotesessions.UpstreamToken{
		Token:                              "synthetic-token",
		Resource:                           "",
		CredentialOwner:                    remotesessions.CredentialOwnerSubject,
		RemoteSessionClientID:              clientID,
		RemoteSessionID:                    grantID,
		RemoteSessionUpdatedAt:             time.Time{},
		RemoteSessionResolvedFromUpdatedAt: time.Time{},
		GrantGeneration:                    3,
		AccessExpiresAt:                    &expiresAt,
		ClientCredentialErr:                nil,
	}
	self := subject
	self.CredentialOwner = remotesessions.CredentialOwnerSelf
	self.RemoteSessionID = uuid.Nil
	self.GrantGeneration = 0
	noExpiry := subject
	noExpiry.AccessExpiresAt = nil
	// identitychaining wraps a chained token as a subject token from no grant.
	chained := subject
	chained.RemoteSessionClientID, chained.RemoteSessionID, chained.GrantGeneration, chained.AccessExpiresAt = uuid.Nil, uuid.Nil, 0, nil
	noGrant := subject
	noGrant.RemoteSessionID = uuid.Nil
	firstGeneration := subject
	firstGeneration.GrantGeneration = 0
	empty := subject
	empty.Token = ""
	unknownOwner := subject
	unknownOwner.CredentialOwner = ""

	for _, tc := range []struct {
		name     string
		token    remotesessions.UpstreamToken
		expected *identity.UpstreamCredential
	}{
		{name: "subject", token: subject, expected: &identity.UpstreamCredential{Owner: identity.OwnerSubject, ClientID: clientID.String(), GrantID: grantID.String(), GrantGeneration: 3, TokenSHA256: hash, TokenExpiresAt: new(expiresAt.Unix())}},
		{name: "subject without stated expiry", token: noExpiry, expected: &identity.UpstreamCredential{Owner: identity.OwnerSubject, ClientID: clientID.String(), GrantID: grantID.String(), GrantGeneration: 3, TokenSHA256: hash, TokenExpiresAt: nil}},
		{name: "self", token: self, expected: &identity.UpstreamCredential{Owner: identity.OwnerSelf, ClientID: clientID.String(), GrantID: "", GrantGeneration: 0, TokenSHA256: hash, TokenExpiresAt: new(expiresAt.Unix())}},
		{name: "identity chained", token: chained, expected: nil},
		{name: "subject without grant row", token: noGrant, expected: nil},
		{name: "subject without generation", token: firstGeneration, expected: nil},
		{name: "no token", token: empty, expected: nil},
		{name: "unknown owner", token: unknownOwner, expected: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, upstreamProvenance(tc.token))
		})
	}
}
