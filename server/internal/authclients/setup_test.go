package authclients_test

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

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *authclients.Service
	conn           *pgxpool.Pool
	enc            *encryption.Client
	organizationID string
	projectID      uuid.UUID
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential})
	enc := testenv.NewEncryptionClient(t)
	svc := authclients.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), enc)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		enc:            enc,
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      *authCtx.ProjectID,
	}
}

func withOrgGrants(t *testing.T, ctx context.Context, scopes ...authz.Scope) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	grants := make([]authz.Grant, 0, len(scopes))
	for _, scope := range scopes {
		grants = append(grants, authz.NewGrant(scope, authCtx.ActiveOrganizationID))
	}
	return authztest.WithExactGrants(t, ctx, grants...)
}

type serverFixture struct {
	id        uuid.UUID
	projectID uuid.UUID
	remoteID  uuid.UUID
}

func seedServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, serverVisibility string) serverFixture {
	t.Helper()

	queries := testrepo.New(conn)
	remoteID := uuid.New()
	require.NoError(t, queries.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{
		ID:        remoteID,
		ProjectID: projectID,
		Url:       "https://upstream.example.com/" + remoteID.String(),
	}))

	serverID := uuid.New()
	require.NoError(t, queries.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         projectID,
		Slug:              conv.ToPGText("srv-" + serverID.String()[:8]),
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
		Visibility:        serverVisibility,
	}))

	return serverFixture{id: serverID, projectID: projectID, remoteID: remoteID}
}

func seedStaticClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()

	queries := testrepo.New(conn)
	clientID, err := queries.CreateAuthClientFixture(ctx, testrepo.CreateAuthClientFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		ClientType:     authz.AuthClientTypeStaticCredential,
		Name:           name,
	})
	require.NoError(t, err)

	require.NoError(t, queries.CreateAuthClientHeaderFixture(ctx, testrepo.CreateAuthClientHeaderFixtureParams{
		AuthClientID:   clientID,
		OrganizationID: organizationID,
		Name:           "Authorization",
		IsSecret:       true,
		Value:          conv.ToPGText("Bearer super-secret-value"),
	}))

	return clientID
}

func associate(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, server serverFixture, clientID uuid.UUID) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).AssociateAuthClientFixture(ctx, testrepo.AssociateAuthClientFixtureParams{
		ProjectID:      server.projectID,
		OrganizationID: organizationID,
		McpServerID:    server.id,
		AuthClientID:   clientID,
	}))
}

func seedOtherOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) (string, uuid.UUID) {
	t.Helper()

	queries := testrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	require.NoError(t, queries.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 organizationID,
		Name:               organizationID,
		Slug:               organizationID,
		GramAccountType:    "free",
		WorkosID:           pgtype.Text{String: "", Valid: false},
		Whitelisted:        false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(time.Now().Add(24 * time.Hour)),
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          pgtype.Timestamptz{},
	}))

	slug := "p-" + uuid.NewString()[:8]
	projectID, err := queries.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             uuid.New(),
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return organizationID, projectID
}

// seedRegistration creates an OAuth registration in projectID. owner is the
// registration's credential owner: subject or self.
func seedRegistration(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, owner string) uuid.UUID {
	t.Helper()

	queries := remotesessionsrepo.New(conn)
	suffix := uuid.NewString()[:8]
	issuer, err := queries.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              "upstream-" + suffix,
		Issuer:                            "https://" + suffix + ".upstream.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://" + suffix + ".upstream.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + suffix + ".upstream.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token", "client_credentials"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)

	registration, err := queries.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:          conv.ToPGText(organizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "client-" + suffix,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		CredentialOwner:         conv.ToPGText(owner),
	})
	require.NoError(t, err)

	return registration.ID
}

func seedProject(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	slug := "p-" + uuid.NewString()[:8]
	projectID, err := testrepo.New(ti.conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             uuid.New(),
		Name:           slug,
		Slug:           slug,
		OrganizationID: ti.organizationID,
	})
	require.NoError(t, err)
	return projectID
}
