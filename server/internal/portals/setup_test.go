package portals_test

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/portals"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

// testSiteURL is the deterministic public-facing site URL used by the
// portals service in tests. Matches the pattern other service tests use
// (e.g. mcpmetadata/setup_test.go uses "http://0.0.0.0").
const testSiteURL = "http://0.0.0.0"

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
	service        *portals.Service
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

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	svc := portals.NewService(logger, tracerProvider, conn, sessionManager, authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), cache.NoopCache), testSiteURL)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		sessionManager: sessionManager,
	}
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// isHTTPStatus checks if an oops error maps to the given HTTP status.
func isHTTPStatus(err error, status int) bool {
	var oopsErr *oops.ShareableError
	if !errors.As(err, &oopsErr) {
		return false
	}
	return oopsErr.HTTPStatus() == status
}

// seedMcpServerAndEndpoint creates a remote_mcp_server + mcp_server + mcp_endpoint
// row directly through the generated repos for test seeding.
// Returns the endpoint slug.
func seedMcpServerAndEndpoint(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, endpointSlug string) {
	t.Helper()

	server := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://test.example.com/mcp/" + uuid.NewString(),
	})

	mcpServerID, err := uuid.NewV7()
	require.NoError(t, err)
	frontend, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                mcpServerID,
		ProjectID:         projectID,
		Name:              conv.ToPGText("Test MCP Server"),
		Slug:              conv.ToPGText("test-mcp-server-" + mcpServerID.String()[len(mcpServerID.String())-4:]),
		EnvironmentID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
		ToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:        "disabled",
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:      projectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    frontend.ID,
		Slug:           endpointSlug,
	})
	require.NoError(t, err)
}

// seedAsset inserts an asset row for the given project and returns its UUID.
// The asset is not backed by real storage — getPortal only constructs a URL
// from the UUID and does not fetch bytes, so storage is not required for
// these tests.
func seedAsset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	asset, err := assetsrepo.New(conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          "portal-test-asset.png",
		Url:           "memory://portal-test-asset-" + uuid.NewString(),
		ProjectID:     projectID,
		Sha256:        "portal-test-" + uuid.NewString(),
		Kind:          "image",
		ContentType:   "image/png",
		ContentLength: 32,
	})
	require.NoError(t, err)
	return asset.ID
}

// seedProjectLogoAsset inserts an asset row for the project and points the
// project's logo_asset_id at it. Returns the asset's UUID.
func seedProjectLogoAsset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	assetID := seedAsset(t, ctx, conn, projectID)

	_, err := projectsrepo.New(conn).UploadProjectLogo(ctx, projectsrepo.UploadProjectLogoParams{
		LogoAssetID: uuid.NullUUID{UUID: assetID, Valid: true},
		ProjectID:   projectID,
	})
	require.NoError(t, err)

	return assetID
}

// newSiblingOrgContext creates a brand-new organization (different from the
// one in ctx), seeds a user and project for it, and returns a context
// authenticated as that org along with the sessionManager for reuse.
func newSiblingOrgContext(t *testing.T, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	ctx := t.Context()

	orgID := "org-sibling-" + uuid.NewString()[:8]
	orgSlug := "sibling-" + uuid.NewString()[:8]

	orgQueries := orgRepo.New(conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        orgSlug,
		Slug:        orgSlug,
		WorkosID:    pgtype.Text{String: orgID, Valid: true},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	userID := mockidp.MockUserID + "-sibling-" + uuid.NewString()[:4]
	userEmail := "sibling-" + uuid.NewString()[:4] + "@test.example.com"
	usersQueries := userRepo.New(conn)
	_, err = usersQueries.UpsertUser(ctx, userRepo.UpsertUserParams{
		ID:          userID,
		Email:       userEmail,
		DisplayName: "Sibling User",
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgQueries.UpsertOrganizationUserRelationship(ctx, orgRepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	sessionID := uuid.New().String()
	session := sessions.Session{
		SessionID:            sessionID,
		UserID:               userID,
		ActiveOrganizationID: orgID,
		WorkOSSessionID:      "",
	}
	err = sessionManager.StoreSession(ctx, session)
	require.NoError(t, err)

	ctx, err = sessionManager.Authenticate(ctx, sessionID)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectSlug := "sibling-proj-" + uuid.NewString()[:8]
	p, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authCtx.ProjectID = &p.ID
	authCtx.ProjectSlug = &p.Slug
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	return ctx
}

// withNoAuthzGrants returns a context with enterprise RBAC but no grants
// (simulates a session with no project:write permission).
func withNoAuthzGrants(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	// authztest.WithExactGrants with no grants = enterprise RBAC active, nothing allowed
	return authztest.WithExactGrants(t, ctx)
}
