package platformmcp

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
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

// memberMCPURLFixture is an organization with one MCP server that a member
// may connect to, and a plugins service that builds that member's links.
type memberMCPURLFixture struct {
	conn         *pgxpool.Pool
	service      *PluginsService
	principal    Principal
	project      ResolvedProject
	mcpID        uuid.UUID
	endpointSlug string
}

// newMemberMCPURLFixture also returns a context acting as that member.
func newMemberMCPURLFixture(t *testing.T, name string) (context.Context, memberMCPURLFixture) {
	t.Helper()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
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
	service := NewPluginsService(conn, allowBudget(), name+"-key").
		WithAuthorization(authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)).
		withMemberMCPConnectionReader(testMemberMCPConnectionReader{}).
		WithInstallLinks(serverURL, serverURL)
	// A session makes the authorization engine enforce, so both tools check the
	// mcp:connect grant rather than skipping authorization.
	sessionID := "session-" + name
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, OrganizationSlug: "example-org", SessionID: &sessionID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, mcpID.String())})
	return ctx, memberMCPURLFixture{conn: conn, service: service, principal: principal, project: project, mcpID: mcpID, endpointSlug: target.EndpointSlug}
}

// testOrganizationHosts resolves stored default hosts to the canonical host or
// the extra platform host.
func testOrganizationHosts(t *testing.T) *orghost.Resolver {
	t.Helper()
	serverURL, err := url.Parse(testCanonicalBaseURL)
	require.NoError(t, err)
	extraHost, err := url.Parse(testExtraHostBaseURL)
	require.NoError(t, err)
	return orghost.New(orghost.Config{ServerURL: serverURL, SiteURL: serverURL, PlatformHosts: map[string]string{extraHost.Host: testExtraHostBaseURL}, LegacyDefaultHost: nil, NewOrganizationDefaultHost: nil})
}

// Member install and connection-status URLs point at the platform host the
// request arrived on. Custom domains, private networks, and requests without
// an origin keep the configured server URL.
func TestMemberMCPURLsFollowPlatformHost(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberMCPURLFixture(t, "platform_mcp_member_urls_platform_host")

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
			wantURL := tc.wantBase + "/mcp/" + f.endpointSlug

			install, err := f.service.GetMyInstallInstructions(requestCtx, f.principal, GetMyInstallInstructionsInput{ProjectID: f.project.ID.String(), Plugin: "", MCPID: f.mcpID.String(), ClientFamily: OnboardingClientClaudeCode})
			require.NoError(t, err)
			require.True(t, install.Supported)
			require.NotEmpty(t, install.Instructions)
			require.Equal(t, wantURL, install.Instructions[0].URL)

			status, err := f.service.GetMyMCPConnectionStatus(requestCtx, f.principal, GetMyMCPStatusInput{ProjectID: f.project.ID.String(), MCPID: f.mcpID.String()})
			require.NoError(t, err)
			require.Equal(t, wantURL, status.ConnectionURL)
		})
	}
}

// Member install and connection-status URLs use the platform host stored as
// the organization's default host, which is where the dashboard serves the
// organization and so the host of its Hosted Address, even when the Platform
// MCP request arrived on another platform host.
func TestMemberMCPURLsFollowOrganizationDefaultHost(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberMCPURLFixture(t, "platform_mcp_member_urls_org_default_host")
	require.NoError(t, organizationsrepo.New(f.conn).SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{ID: f.principal.OrganizationID, DefaultHost: pgtype.Text{String: testExtraHostBaseURL, Valid: true}}))
	service := f.service.WithOrganizationHosts(testOrganizationHosts(t), testenv.NewLogger(t))
	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: testCanonicalBaseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
	wantURL := testExtraHostBaseURL + "/mcp/" + f.endpointSlug

	install, err := service.GetMyInstallInstructions(ctx, f.principal, GetMyInstallInstructionsInput{ProjectID: f.project.ID.String(), Plugin: "", MCPID: f.mcpID.String(), ClientFamily: OnboardingClientCursor})
	require.NoError(t, err)
	require.True(t, install.Supported)
	require.NotEmpty(t, install.Instructions)
	require.Equal(t, wantURL, install.Instructions[0].URL)

	status, err := service.GetMyMCPConnectionStatus(ctx, f.principal, GetMyMCPStatusInput{ProjectID: f.project.ID.String(), MCPID: f.mcpID.String()})
	require.NoError(t, err)
	require.Equal(t, wantURL, status.ConnectionURL)

	plugin, err := service.pluginInstallInstructions(ctx, f.principal.OrganizationID, OnboardingClientCursor, "Example plugin")
	require.NoError(t, err)
	require.NotEmpty(t, plugin.Instructions)
	require.Equal(t, testExtraHostBaseURL+"/example-org/device-agent", plugin.Instructions[0].URL)

	// A private network caller may not reach a platform host, so it keeps the
	// configured server URL.
	privateCtx := requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})
	private, err := service.GetMyMCPConnectionStatus(privateCtx, f.principal, GetMyMCPStatusInput{ProjectID: f.project.ID.String(), MCPID: f.mcpID.String()})
	require.NoError(t, err)
	require.Equal(t, testCanonicalBaseURL+"/mcp/"+f.endpointSlug, private.ConnectionURL)
}

// Without a stored default host, the device-agent link follows the platform
// host the request arrived on.
func TestPluginDeviceAgentURLFollowsPlatformHostWithoutDefaultHost(t *testing.T) {
	t.Parallel()

	ctx, f := newMemberMCPURLFixture(t, "platform_mcp_member_urls_no_default_host")
	service := f.service.WithOrganizationHosts(testOrganizationHosts(t), testenv.NewLogger(t))
	ctx = requestorigin.WithContext(ctx, requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: testExtraHostBaseURL, OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil})

	plugin, err := service.pluginInstallInstructions(ctx, f.principal.OrganizationID, OnboardingClientCursor, "Example plugin")
	require.NoError(t, err)
	require.NotEmpty(t, plugin.Instructions)
	require.Equal(t, testExtraHostBaseURL+"/example-org/device-agent", plugin.Instructions[0].URL)
}
