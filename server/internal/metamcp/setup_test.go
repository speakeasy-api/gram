package metamcp_test

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

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	unproxiedmcprepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true, Temporal: false})
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
	service        *metamcp.Service
	conn           *pgxpool.Pool
	sessionManager *sessions.Manager
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

	auditLogger := audit.NewLogger()

	svc := metamcp.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()), auditLogger, nil)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func withExactAuthzGrants(t *testing.T, ctx context.Context, conn *pgxpool.Pool, grants ...authz.Grant) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	authCtx.AccountType = "enterprise"
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "metamcp-rbac-grants-"+uuid.NewString())
	for _, grant := range grants {
		selectors, err := grant.Selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
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

// seedUserSessionIssuer inserts a user_session_issuers row in the given
// project.
func seedUserSessionIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          projectID,
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	return issuer.ID
}

// seedMcpServer creates a remote_mcp_server + mcp_server row directly through
// the generated repos so membership tests have a valid mcp_server_id FK.
func seedMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	return seedMcpServerFronting(t, ctx, conn, projectID, mcpserversrepo.CreateMCPServerParams{
		RemoteMcpServerID: conv.ToNullUUID(seedRemoteBackend(t, ctx, conn, projectID)),
	})
}

// seedMcpServerFronting creates an mcp_server row on the caller's chosen
// backend so tests can point two servers at one backend.
func seedMcpServerFronting(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, backend mcpserversrepo.CreateMCPServerParams) uuid.UUID {
	t.Helper()

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)

	backend.ID = mcpServerID
	backend.ProjectID = projectID
	backend.Name = conv.ToPGText("member mcp server")
	backend.Slug = conv.ToPGText("member-mcp-server-" + uuid.NewString())
	backend.UserSessionIssuerID = conv.ToNullUUID(seedUserSessionIssuer(t, ctx, conn, projectID))
	backend.Visibility = "disabled"

	frontend, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, backend)
	require.NoError(t, err)

	return frontend.ID
}

// seedRemoteBackend inserts a remote_mcp_servers row.
func seedRemoteBackend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	server := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})

	return server.ID
}

// seedTunnelBackend inserts a tunneled_mcp_servers row.
func seedTunnelBackend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	server, err := tunneledmcprepo.New(conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "tunnel " + uuid.NewString(),
		KeyHash:   "key-hash-" + uuid.NewString(),
		KeyPrefix: "key-prefix",
	})
	require.NoError(t, err)

	return server.ID
}

// seedToolsetBackend inserts a toolsets row.
func seedToolsetBackend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	slug := "toolset-" + uuid.NewString()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           slug,
		Slug:           slug,
		McpSlug:        conv.ToPGText(slug),
		McpEnabled:     true,
	})
	require.NoError(t, err)

	return toolset.ID
}

// seedUnproxiedBackend inserts an unproxied_mcp_servers row.
func seedUnproxiedBackend(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	server, err := unproxiedmcprepo.New(conn).CreateServer(ctx, unproxiedmcprepo.CreateServerParams{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Name:        pgtype.Text{String: "", Valid: false},
		Slug:        conv.ToPGText("unproxied-" + uuid.NewString()),
		Url:         "https://vendor.example.com/mcp",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return server.ID
}

// seedOtherProject creates an additional project in the caller's organization.
// Used to exercise cross-project ownership rejection.
func seedOtherProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return otherProject.ID
}
