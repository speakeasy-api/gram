package remotesessions_test

import (
	"context"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/environments"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

// testServerURL is the default-domain base URL the test service derives MCP
// URLs from when migrating legacy client registrations.
const testServerURL = "https://app.getgram.ai"

type testInstance struct {
	service        *remotesessions.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	envEntries     *environments.EnvironmentEntries
	redisCache     *cache.RedisCacheAdapter
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	enc := testenv.NewEncryptionClient(t)
	envEntries := environments.NewEnvironmentEntries(logger, conn, enc, mcpmetadatarepo.New(conn))

	serverURL, err := url.Parse(testServerURL)
	require.NoError(t, err)

	redisCache := cache.NewRedisCacheAdapter(redisClient)

	svc := remotesessions.NewService(
		logger,
		tracerProvider,
		conn,
		sessionManager,
		authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		enc,
		envEntries,
		guardianPolicy,
		audit.NewLogger(),
		serverURL,
		redisCache,
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		envEntries:     envEntries,
		redisCache:     redisCache,
	}
}

// withExactAccessGrants installs exactly the supplied grant set on a fresh
// principal and returns a context bound to those grants. Use it to exercise
// RBAC-deny paths.
func withExactAccessGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "remotesessions-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, _ := grant.Selector.MarshalJSON()
		_, err := accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			PrincipalUrn:   principal,
			Scope:          string(grant.Scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	loadedGrants, err := authz.LoadGrants(ctx, conn, authCtx.ActiveOrganizationID, []urn.Principal{principal})
	require.NoError(t, err)

	return authz.GrantsToContext(ctx, loadedGrants)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func createRemoteClient(t *testing.T, ctx context.Context, ti *testInstance, issuerID, userIssuerID, clientID string) string {
	t.Helper()
	created, err := ti.service.CreateRemoteSessionClient(ctx, &clientsgen.CreateRemoteSessionClientPayload{
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerIds:  []string{userIssuerID},
		ClientID:              clientID,
		ClientSecret:          nil,
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	return created.ID
}

func insertRemoteSession(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal urn.SessionSubject, userIssuerID, clientID string) repo.RemoteSession {
	t.Helper()
	userIssuerUUID, err := uuid.Parse(userIssuerID)
	require.NoError(t, err)
	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)

	row, err := repo.New(conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            principal,
		UserSessionIssuerID:   userIssuerUUID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  "ciphertext",
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return row
}

// seedUser inserts a users row so a `user:<id>` subject URN resolves to a
// display name and email when listing remote sessions.
func seedUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, id, email, displayName string) {
	t.Helper()
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
}

func createUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

func countRemoteSessionClientUserSessionIssuerBindings(t *testing.T, ctx context.Context, conn *pgxpool.Pool, clientID, userIssuerID uuid.UUID) int {
	t.Helper()

	count, err := repo.New(conn).CountRemoteSessionClientUserSessionIssuerBindings(ctx, repo.CountRemoteSessionClientUserSessionIssuerBindingsParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userIssuerID,
	})
	require.NoError(t, err)
	return int(count)
}

// remoteSessionClientOrganizationID reads the persisted organization_id of a
// remote_session_client by id (scoped to its project).
func remoteSessionClientOrganizationID(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, clientID uuid.UUID) string {
	t.Helper()

	row, err := repo.New(conn).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:        clientID,
		ProjectID: projectID,
	})
	require.NoError(t, err)

	return row.RemoteSessionClient.OrganizationID.String
}

// createProject creates a second project in the test's organization so tests
// can exercise cross-project (cross-tenant) rejection paths.
func createProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	p, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	return p.ID
}

// createUserSessionIssuerInProject creates a user session issuer owned by an
// arbitrary project rather than the one on the auth context.
func createUserSessionIssuerInProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               slug,
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true},
	})
	require.NoError(t, err)
	return issuer.ID
}

// createRemoteIssuerInProject creates a remote session issuer owned by an
// arbitrary project rather than the one on the auth context.
func createRemoteIssuerInProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedOrgLevelRemoteIssuer creates an organization-level (cross-project,
// project_id IS NULL) remote session issuer owned by the supplied organization.
// Org-level issuers are addressed only by id; pass the caller's active org to
// mint an inherited issuer or a different org to exercise cross-org isolation.
func seedOrgLevelRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) uuid.UUID {
	t.Helper()
	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedOrgLevelRemoteClient creates an organization-level (project_id IS NULL,
// organization_id set) remote_session_client referencing remoteIssuerID and
// attaches it to each userSessionIssuerID through the join table. Org-level
// clients carry no project; any project in the org can bind one to its own
// user_session_issuer.
func seedOrgLevelRemoteClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, remoteIssuerID uuid.UUID, clientID string, userSessionIssuerIDs ...uuid.UUID) uuid.UUID {
	t.Helper()
	q := repo.New(conn)
	created, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        conv.ToPGText(organizationID),
		RemoteSessionIssuerID: remoteIssuerID,
		ClientID:              clientID,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
	})
	require.NoError(t, err)
	for _, usi := range userSessionIssuerIDs {
		require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: created.ID,
			UserSessionIssuerID:   usi,
		}))
	}
	return created.ID
}

// seedMCPServerInOrg creates a project in the supplied organization and an MCP
// server within it, returning the MCP server id. Used to exercise cross-org
// isolation on org-admin MCP server lookups.
func seedMCPServerInOrg(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) uuid.UUID {
	t.Helper()
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	// MCP servers require exactly one backend; seed a remote MCP server to satisfy
	// the backend-exclusivity constraint.
	remoteServer, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     project.ID,
		TransportType: "sse",
		Url:           "https://mcp.example.com",
	})
	require.NoError(t, err)

	// A private remote MCP server requires a user_session_issuer (DB CHECK
	// constraint); mint one owned by the same project.
	issuerID := createUserSessionIssuerInProject(t, ctx, conn, project.ID, "usi-"+uuid.NewString()[:8])

	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           project.ID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
	return server.ID
}

// createOrganization seeds a second organization_metadata row so tests can
// exercise cross-organization isolation. Returns the new organization id.
func createOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) string {
	t.Helper()
	orgID := uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        slug,
		Slug:        slug,
		WorkosID:    pgtype.Text{String: orgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

func createRemoteIssuer(t *testing.T, ctx context.Context, svc *testInstance, slug, regEndpoint string) string {
	t.Helper()
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	regEP := regEndpoint
	created, err := svc.service.CreateRemoteSessionIssuer(ctx, &issuersgen.CreateRemoteSessionIssuerPayload{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              &regEP,
		JwksURI:                           nil,
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	return created.ID
}

// withAdmin returns ctx with the auth context's IsAdmin flag flipped to true.
// Tests for admin-only endpoints opt in explicitly so non-admin paths exercise
// the realistic default produced by testenv.InitAuthContext.
func withAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, authCtx)
}

// insertProxyProvider seeds an oauth_proxy_server + oauth_proxy_provider row
// with the supplied secrets JSONB for the clone tests. Returns the provider
// id and the oauth_proxy_server id (the latter for attaching toolsets).
func insertProxyProvider(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug, providerType string, secrets []byte) (uuid.UUID, uuid.UUID) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := oauthrepo.New(conn)
	srv, err := q.UpsertOAuthProxyServer(ctx, oauthrepo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "srv-" + slug,
		Audience:  conv.ToPGText("https://example.com"),
	})
	require.NoError(t, err)

	prov, err := q.UpsertOAuthProxyProvider(ctx, oauthrepo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                srv.ID,
		Slug:                              slug,
		ProviderType:                      providerType,
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RegistrationEndpoint:              conv.ToPGText("https://idp.example.com/register"),
		ScopesSupported:                   []string{"openid"},
		ResponseTypesSupported:            []string{"code"},
		ResponseModesSupported:            []string{"query"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		SecurityKeyNames:                  []string{},
		Secrets:                           secrets,
	})
	require.NoError(t, err)
	return prov.ID, srv.ID
}

// attachToolsetToProxyServer seeds a toolset routed through the given
// oauth_proxy_server under the given MCP slug. When customDomain is non-empty
// a custom_domains row is created and bound to the toolset, making the MCP
// server reachable on both the default domain and the custom domain.
func attachToolsetToProxyServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, proxyServerID uuid.UUID, mcpSlug, customDomain string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	q := toolsetsrepo.New(conn)
	_, err := q.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   mcpSlug,
		Slug:                   mcpSlug,
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(mcpSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = q.UpdateToolsetOAuthProxyServer(ctx, toolsetsrepo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: proxyServerID, Valid: true},
		Slug:               mcpSlug,
		ProjectID:          *authCtx.ProjectID,
	})
	require.NoError(t, err)

	if customDomain != "" {
		domainRow, err := customdomainsrepo.New(conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			Domain:          customDomain,
			IngressName:     pgtype.Text{},
			CertSecretName:  pgtype.Text{},
			ProvisionerKind: "ingress",
			IpAllowlist:     []string{},
		})
		require.NoError(t, err)

		err = q.SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
			CustomDomainID: uuid.NullUUID{UUID: domainRow.ID, Valid: true},
			Slug:           mcpSlug,
			ProjectID:      *authCtx.ProjectID,
		})
		require.NoError(t, err)
	}
}

// seedLegacyRegistration stores a legacy OAuth proxy client registration in
// Redis through the real oauth typed cache — the same write path as the
// legacy DCR endpoint — pinning the wire format the clone migration reads.
func seedLegacyRegistration(t *testing.T, ctx context.Context, ti *testInstance, info oauth.OauthProxyClientInfo) {
	t.Helper()
	typed := cache.NewTypedObjectCache[oauth.OauthProxyClientInfo](testenv.NewLogger(t), ti.redisCache, cache.SuffixNone)
	require.NoError(t, typed.Store(ctx, info))
}

// seedEnvironmentWithEntries creates an environment + entries via the same
// EnvironmentEntries helper the production code uses, so values land encrypted
// under the test encryption key. Returns the environment slug.
func seedEnvironmentWithEntries(t *testing.T, ctx context.Context, ti *testInstance, slug string, entries map[string]string) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	envRow, err := environmentsrepo.New(ti.conn).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           slug,
		Slug:           slug,
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	values := make([]string, 0, len(entries))
	for name, value := range entries {
		names = append(names, name)
		values = append(values, value)
	}
	_, err = ti.envEntries.CreateEnvironmentEntries(ctx, environmentsrepo.CreateEnvironmentEntriesParams{
		EnvironmentID: envRow.ID,
		Names:         names,
		Values:        values,
	})
	require.NoError(t, err)
	return envRow.Slug
}
