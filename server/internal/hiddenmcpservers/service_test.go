package hiddenmcpservers_test

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

	srv "github.com/speakeasy-api/gram/server/gen/hidden_mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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

type instance struct {
	svc     *hiddenmcpservers.Service
	conn    *pgxpool.Pool
	authCtx *contextvalues.AuthContext
}

func newTestService(t *testing.T) (context.Context, *instance) {
	t.Helper()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := hiddenmcpservers.NewService(logger, tracerProvider, conn, sessionManager, authzEngine)
	return ctx, &instance{svc: svc, conn: conn, authCtx: authCtx}
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func asUser(ctx context.Context, si *instance, userID string) context.Context {
	other := *si.authCtx
	other.UserID = userID
	return contextvalues.SetAuthContext(ctx, &other)
}

func createProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) projectsrepo.Project {
	t.Helper()
	slug := "hidden-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: orgID})
	require.NoError(t, err)
	return project
}

func createOtherOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             orgID,
		Name:           "Other Org",
		Slug:           "other-" + uuid.NewString()[:8],
		WorkosID:       conv.ToPGText(orgID),
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

func createServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, project projectsrepo.Project, slug, visibility string) uuid.UUID {
	t.Helper()
	remote := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ID:            uuid.Nil,
		ProjectID:     project.ID,
		Name:          pgtype.Text{String: "", Valid: false},
		Slug:          pgtype.Text{String: "", Valid: false},
		TransportType: "streamable-http",
		Url:           "https://member.example.com/mcp/" + uuid.NewString(),
	})
	issuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:               project.ID,
		OrganizationID:          conv.ToPGText(project.OrganizationID),
		Slug:                    "issuer-" + uuid.NewString(),
		AuthnChallengeMode:      "chain",
		SessionDuration:         pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
		AuthorizationServerMode: pgtype.Text{String: "", Valid: false},
		UseAuthenticationHost:   pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    uuid.Must(uuid.NewV7()),
		ProjectID:             project.ID,
		Name:                  conv.ToPGText("Server " + slug),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return server.ID
}

func hide(t *testing.T, ctx context.Context, si *instance, serverID uuid.UUID) {
	t.Helper()
	require.NoError(t, si.svc.Hide(ctx, &srv.HidePayload{SessionToken: nil, McpServerID: serverID.String()}))
}

func listedIDs(t *testing.T, ctx context.Context, si *instance) []string {
	t.Helper()
	result, err := si.svc.List(ctx, &srv.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	ids := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		ids = append(ids, server.McpServerID)
	}
	return ids
}

func TestHideListsServerAndUnhideRemovesIt(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "docs", mcpservers.VisibilityPublic)
	createServer(t, ctx, si.conn, project, "wiki", mcpservers.VisibilityPublic)

	require.Empty(t, listedIDs(t, ctx, si))
	hide(t, ctx, si, serverID)

	result, err := si.svc.List(ctx, &srv.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	listed := result.Servers[0]
	require.Equal(t, serverID.String(), listed.McpServerID)
	require.Equal(t, project.ID.String(), listed.ProjectID)
	require.Equal(t, project.Slug, listed.ProjectSlug)
	require.Equal(t, "docs", conv.PtrValOrEmpty(listed.Slug, ""))
	require.Equal(t, "Server docs", conv.PtrValOrEmpty(listed.Name, ""))
	_, err = time.Parse(time.RFC3339, listed.HiddenAt)
	require.NoError(t, err)

	require.NoError(t, si.svc.Unhide(ctx, &srv.UnhidePayload{SessionToken: nil, McpServerID: serverID.String()}))
	require.Empty(t, listedIDs(t, ctx, si))
	// Unhiding a server that is not hidden is a no-op.
	require.NoError(t, si.svc.Unhide(ctx, &srv.UnhidePayload{SessionToken: nil, McpServerID: serverID.String()}))
}

func TestHideIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "docs", mcpservers.VisibilityPublic)

	hide(t, ctx, si, serverID)
	hide(t, ctx, si, serverID)

	require.Equal(t, []string{serverID.String()}, listedIDs(t, ctx, si))
}

func TestHideServerInAnotherOrganizationIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	otherOrg := createOtherOrganization(t, ctx, si.conn)
	foreign := createServer(t, ctx, si.conn, createProject(t, ctx, si.conn, otherOrg), "docs", mcpservers.VisibilityPublic)

	err := si.svc.Hide(ctx, &srv.HidePayload{SessionToken: nil, McpServerID: foreign.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	require.Empty(t, listedIDs(t, ctx, si))
	for _, orgID := range []string{si.authCtx.ActiveOrganizationID, otherOrg} {
		hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, si.conn, orgID, si.authCtx.UserID)
		require.NoError(t, err)
		require.Empty(t, hidden)
	}
}

func TestHideUnknownServerIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	err := si.svc.Hide(ctx, &srv.HidePayload{SessionToken: nil, McpServerID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestHiddenServersArePerUser(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "docs", mcpservers.VisibilityPublic)
	otherCtx := asUser(ctx, si, "user-"+uuid.NewString())

	hide(t, ctx, si, serverID)

	require.Equal(t, []string{serverID.String()}, listedIDs(t, ctx, si))
	require.Empty(t, listedIDs(t, otherCtx, si))

	// Another user unhiding leaves the first user's preference alone.
	require.NoError(t, si.svc.Unhide(otherCtx, &srv.UnhidePayload{SessionToken: nil, McpServerID: serverID.String()}))
	require.Equal(t, []string{serverID.String()}, listedIDs(t, ctx, si))
}

func TestHideRejectsInvalidServerID(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	requireOopsCode(t, si.svc.Hide(ctx, &srv.HidePayload{SessionToken: nil, McpServerID: "not-a-uuid"}), oops.CodeBadRequest)
	requireOopsCode(t, si.svc.Unhide(ctx, &srv.UnhidePayload{SessionToken: nil, McpServerID: "not-a-uuid"}), oops.CodeBadRequest)
}

func TestDeletedServerIsNotListedOrHideable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "docs", mcpservers.VisibilityPublic)
	hide(t, ctx, si, serverID)

	_, err := mcpserversrepo.New(si.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: serverID, ProjectID: project.ID})
	require.NoError(t, err)

	require.Empty(t, listedIDs(t, ctx, si))
	err = si.svc.Hide(ctx, &srv.HidePayload{SessionToken: nil, McpServerID: serverID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSupportSessionCannotChangeHiddenServers(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "docs", mcpservers.VisibilityPublic)
	hide(t, ctx, si, serverID)

	support := *si.authCtx
	support.IsAdmin = true
	support.SupportOrganizationID = si.authCtx.ActiveOrganizationID
	supportCtx := contextvalues.WithValidatedSupportSession(ctx, &support)

	requireOopsCode(t, si.svc.Hide(supportCtx, &srv.HidePayload{SessionToken: nil, McpServerID: serverID.String()}), oops.CodeForbidden)
	requireOopsCode(t, si.svc.Unhide(supportCtx, &srv.UnhidePayload{SessionToken: nil, McpServerID: serverID.String()}), oops.CodeForbidden)
	require.Equal(t, []string{serverID.String()}, listedIDs(t, supportCtx, si))
}

func TestHidePrivateServerRequiresConnectAccess(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	serverID := createServer(t, ctx, si.conn, project, "private-docs", mcpservers.VisibilityPrivate)

	noAccess := authztest.WithExactGrants(t, ctx)
	err := si.svc.Hide(noAccess, &srv.HidePayload{SessionToken: nil, McpServerID: serverID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, si.conn, si.authCtx.ActiveOrganizationID, si.authCtx.UserID)
	require.NoError(t, err)
	require.Empty(t, hidden)

	connect := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, serverID.String()))
	hide(t, connect, si, serverID)
	require.Equal(t, []string{serverID.String()}, listedIDs(t, connect, si))
}

func TestListOmitsServerCallerCannotConnect(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	project := createProject(t, ctx, si.conn, si.authCtx.ActiveOrganizationID)
	private := createServer(t, ctx, si.conn, project, "private-docs", mcpservers.VisibilityPrivate)
	public := createServer(t, ctx, si.conn, project, "public-docs", mcpservers.VisibilityPublic)
	for _, serverID := range []uuid.UUID{private, public} {
		require.NoError(t, hiddenmcpservers.Hide(ctx, si.conn, si.authCtx.ActiveOrganizationID, si.authCtx.UserID, serverID))
	}

	require.Equal(t, []string{public.String()}, listedIDs(t, authztest.WithExactGrants(t, ctx), si))
	require.ElementsMatch(t, []string{private.String(), public.String()}, listedIDs(t, authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPConnect, private.String())), si))
}
