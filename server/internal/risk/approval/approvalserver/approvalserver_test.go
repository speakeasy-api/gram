package approvalserver_test

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	endpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/approvalserver"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newProject(t *testing.T) (*pgxpool.Pool, *contextvalues.AuthContext) {
	t.Helper()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	authCtx, ok := contextvalues.GetAuthContext(testenv.InitAuthContext(t, t.Context(), conn, sessionManager))
	require.True(t, ok)
	return conn, authCtx
}

func ensure(t *testing.T, conn *pgxpool.Pool, authCtx *contextvalues.AuthContext) approvalserver.Outcome {
	t.Helper()
	tx := testenv.BeginTx(t, t.Context(), conn)
	outcome, err := approvalserver.Ensure(t.Context(), tx, audit.NewLogger(), authCtx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return outcome
}

func TestEnsureCreatesTheServerOnceAndAddsItToTheDefaultPlugin(t *testing.T) {
	t.Parallel()
	conn, authCtx := newProject(t)

	_, found, err := approvalserver.Find(t.Context(), conn, *authCtx.ProjectID)
	require.NoError(t, err)
	require.False(t, found)

	first := ensure(t, conn, authCtx)
	require.True(t, first.Created)
	require.True(t, first.AddedToDefaultPlugin)
	require.NotEqual(t, uuid.Nil, first.Server.ID)
	require.Equal(t, authCtx.OrganizationSlug+"-"+*authCtx.ProjectSlug+"-approvals", first.Server.EndpointSlug)

	found2, ok, err := approvalserver.Find(t.Context(), conn, *authCtx.ProjectID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.Server, found2)

	plugin, err := pluginsrepo.New(conn).GetDefaultPlugin(t.Context(), pluginsrepo.GetDefaultPluginParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	memberParams := pluginsrepo.GetPluginServerByBackendParams{
		PluginID:       plugin.ID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ToolsetID:      uuid.NullUUID{},
		McpServerID:    uuid.NullUUID{UUID: first.Server.ID, Valid: true},
	}
	member, err := pluginsrepo.New(conn).GetPluginServerByBackend(t.Context(), memberParams)
	require.NoError(t, err, "the approvals server joins the Default plugin")
	require.Equal(t, approval.MCPServerName, member.DisplayName)

	// Later calls find the server and change nothing, so an admin who removes
	// it from a plugin is not overruled.
	_, err = pluginsrepo.New(conn).RemovePluginServer(t.Context(), pluginsrepo.RemovePluginServerParams{ID: member.ID, PluginID: plugin.ID})
	require.NoError(t, err)
	second := ensure(t, conn, authCtx)
	require.False(t, second.Created)
	require.False(t, second.AddedToDefaultPlugin)
	require.Equal(t, first.Server, second.Server)
	_, err = pluginsrepo.New(conn).GetPluginServerByBackend(t.Context(), memberParams)
	require.ErrorIs(t, err, pgx.ErrNoRows, "a removed approvals server stays out of the Default plugin")
}

func TestEnsureFitsALongEndpointSlug(t *testing.T) {
	t.Parallel()
	conn, authCtx := newProject(t)

	long := *authCtx
	long.OrganizationSlug = strings.Repeat("o", 150)
	outcome := ensure(t, conn, &long)
	require.True(t, outcome.Created)
	require.Equal(t, strings.Repeat("o", 128), outcome.Server.EndpointSlug)
}

func TestEnsureSkipsTakenEndpointSlugs(t *testing.T) {
	t.Parallel()
	conn, authCtx := newProject(t)

	// Another server holds the slug the approvals server would take first.
	other, err := approvalrepo.New(conn).CreateBuiltinMCPServer(t.Context(), approvalrepo.CreateBuiltinMCPServerParams{
		ID:                uuid.New(),
		ProjectID:         *authCtx.ProjectID,
		Name:              conv.ToPGText("Other"),
		Slug:              conv.ToPGText("other"),
		BuiltinBackend:    conv.ToPGText("other"),
		Visibility:        visibility.Private,
		NetworkAccessMode: networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	require.NoError(t, err)
	base := authCtx.OrganizationSlug + "-" + *authCtx.ProjectSlug + "-approvals"
	_, err = endpointsrepo.New(conn).CreateMCPEndpoint(t.Context(), endpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{},
		McpServerID:     uuid.NullUUID{UUID: other.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{},
		Slug:            base,
	})
	require.NoError(t, err)

	outcome := ensure(t, conn, authCtx)
	require.True(t, outcome.Created)
	id := outcome.Server.ID.String()
	require.Equal(t, base+"-"+id[len(id)-4:], outcome.Server.EndpointSlug)
}

func TestEnsureGivesTheServerItsOwnSignInIssuer(t *testing.T) {
	t.Parallel()
	conn, authCtx := newProject(t)

	outcome := ensure(t, conn, authCtx)
	server, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: outcome.Server.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.True(t, server.UserSessionIssuerID.Valid, "callers without a key sign in through the server's issuer")

	issuer, err := usersessionsrepo.New(conn).GetUserSessionIssuerByID(t.Context(), usersessionsrepo.GetUserSessionIssuerByIDParams{
		ID:             server.UserSessionIssuerID.UUID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Equal(t, "interactive", issuer.AuthnChallengeMode)
	require.Contains(t, issuer.Slug, outcome.Server.Slug)

	// A later call keeps the same issuer.
	ensure(t, conn, authCtx)
	again, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: outcome.Server.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, server.UserSessionIssuerID, again.UserSessionIssuerID)
}
