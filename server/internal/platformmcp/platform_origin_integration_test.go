package platformmcp

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Access tokens are bound to the platform host they were minted for (RFC
// 8707): each host accepts its own tokens and refuses the other's, and
// requests without a platform origin fall back to the configured host.
func TestJWTAuthenticatorBindsTokensToPlatformHost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_jwt_platform_host")
	require.NoError(t, err)

	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "Platform MCP host binding test organization",
		Slug:        "org-" + uuid.NewString()[:8],
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	encryptionClient := testEncryption(t)
	codec, err := NewCredentialCodec(encryptionClient)
	require.NoError(t, err)
	signer := sessiontokens.NewSigner("test-key")
	now := time.Now().UTC()
	store := NewPostgresOAuthStore(conn)
	client := platformoauth.Client{ID: "client-" + uuid.NewString(), Name: "Platform MCP host binding test client", RedirectURIs: []string{"https://client.example.test/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	userID := "user_" + uuid.NewString()
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: userSubjectURN(userID), OrganizationID: organizationID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, store.RegisterConnection(ctx, connection))

	mint := func(baseURL string) string {
		t.Helper()
		resource := baseURL + "/platform-mcp"
		jti, err := codec.Issue(accessJTICredential, organizationID, resource)
		require.NoError(t, err)
		expiresAt := now.Add(time.Hour)
		token, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.SessionSubject{Kind: urn.SessionSubjectKindUser, ID: userID}, Audience: resource, Issuer: resource, ExpiresAt: &expiresAt, ClientID: client.ID, JTI: jti})
		require.NoError(t, err)
		require.NoError(t, store.CreateSession(ctx, platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: jti, RefreshHash: "refresh-" + uuid.NewString(), ExpiresAt: expiresAt, RefreshExpiresAt: expiresAt}))
		return token
	}
	canonicalToken := mint(testCanonicalBaseURL)
	extraToken := mint(testExtraHostBaseURL)

	base, err := url.Parse(testCanonicalBaseURL)
	require.NoError(t, err)
	authenticator, err := NewJWTAuthenticator(signer, conn, encryptionClient, base)
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		surface requestorigin.Surface
		baseURL string
		token   string
		allowed bool
	}{
		{name: "canonical token on canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, token: canonicalToken, allowed: true},
		{name: "canonical token without origin", surface: "", baseURL: "", token: canonicalToken, allowed: true},
		{name: "extra host token on extra host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, token: extraToken, allowed: true},
		{name: "extra host token on canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, token: extraToken, allowed: false},
		{name: "canonical token on extra host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, token: canonicalToken, allowed: false},
		{name: "extra host token on custom domain", surface: requestorigin.SurfaceCustomDomain, baseURL: testExtraHostBaseURL, token: extraToken, allowed: false},
	} {
		requestCtx := ctx
		if tc.surface != "" {
			requestCtx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: tc.surface, BaseURL: tc.baseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
		}
		principal, err := authenticator.Authenticate(requestCtx, tc.token)
		if !tc.allowed {
			require.ErrorIs(t, err, ErrUnauthorized, tc.name)
			continue
		}
		require.NoError(t, err, tc.name)
		require.Equal(t, userID, principal.UserID, tc.name)
		require.Equal(t, organizationID, principal.OrganizationID, tc.name)
	}
}

// Member install and connection-status URLs point at the platform host the
// request arrived on. Custom domains, private networks, and requests without
// an origin keep the configured server URL.
func TestMemberMCPURLsFollowPlatformHost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_member_urls_platform_host")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	rows, err := platformrepo.New(conn).ListPlatformMCPInventoryAuthorizationCandidates(ctx, principal.OrganizationID)
	require.NoError(t, err)
	var mcpID uuid.UUID
	for _, row := range rows {
		if row.ProjectID == project.ID {
			mcpID = row.ID
			break
		}
	}
	require.NotEqual(t, uuid.Nil, mcpID)
	target, err := platformrepo.New(conn).GetPlatformMCPInstallTarget(ctx, platformrepo.GetPlatformMCPInstallTargetParams{OrganizationID: principal.OrganizationID, McpServerID: mcpID, ProjectID: project.ID})
	require.NoError(t, err)
	require.NotEmpty(t, target.EndpointSlug)

	serverURL, err := url.Parse(testCanonicalBaseURL)
	require.NoError(t, err)
	service := NewPluginsService(conn, allowBudget(), "member-urls-platform-host-key").
		WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).
		withMemberMCPConnectionReader(testMemberMCPConnectionReader{}).
		WithInstallLinks(serverURL, serverURL)
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org"}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})

	for _, tc := range []struct {
		name     string
		surface  requestorigin.Surface
		baseURL  string
		wantBase string
	}{
		{name: "canonical host", surface: requestorigin.SurfacePlatform, baseURL: testCanonicalBaseURL, wantBase: testCanonicalBaseURL},
		{name: "extra platform host", surface: requestorigin.SurfacePlatform, baseURL: testExtraHostBaseURL, wantBase: testExtraHostBaseURL},
		{name: "custom domain keeps configured base", surface: requestorigin.SurfaceCustomDomain, baseURL: "https://mcp.customer.example", wantBase: testCanonicalBaseURL},
		{name: "private network keeps configured base", surface: requestorigin.SurfacePrivateNetwork, baseURL: "https://private.example", wantBase: testCanonicalBaseURL},
		{name: "no origin keeps configured base", surface: "", baseURL: "", wantBase: testCanonicalBaseURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requestCtx := ctx
			if tc.surface != "" {
				requestCtx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: tc.surface, BaseURL: tc.baseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
			}
			wantURL := tc.wantBase + "/mcp/" + target.EndpointSlug

			install, err := service.GetMyInstallInstructions(requestCtx, principal, GetMyInstallInstructionsInput{ProjectID: project.ID.String(), Plugin: "", MCPID: mcpID.String(), ClientFamily: OnboardingClientClaudeCode})
			require.NoError(t, err)
			require.True(t, install.Supported)
			require.NotEmpty(t, install.Instructions)
			require.Equal(t, wantURL, install.Instructions[0].URL)

			status, err := service.GetMyMCPConnectionStatus(requestCtx, principal, GetMyMCPStatusInput{ProjectID: project.ID.String(), MCPID: mcpID.String()})
			require.NoError(t, err)
			require.Equal(t, wantURL, status.ConnectionURL)
		})
	}
}
