package remotesessions_test

import (
	"context"
	"log"
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
	"github.com/speakeasy-api/gram/server/internal/environments"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

type testInstance struct {
	service        *remotesessions.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
	envEntries     *environments.EnvironmentEntries
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
	)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
		envEntries:     envEntries,
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
		UserSessionIssuerID:   userIssuerID,
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
// with the supplied secrets JSONB for the clone tests.
func insertProxyProvider(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug, providerType string, secrets []byte) uuid.UUID {
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
	return prov.ID
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
