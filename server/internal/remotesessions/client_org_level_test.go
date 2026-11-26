package remotesessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// TestListClients_OrgLevelClientResolvedForProject proves the load-bearing
// runtime change: an organization-level client (project_id IS NULL) bound to a
// project's user_session_issuer resolves for that project through
// ListRemoteSessionClientsForUserSessionIssuer, the consent + token data source.
func TestListClients_OrgLevelClientResolvedForProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "org-client-issuer")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "org-client-usi")
	seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "org-level-cid", userIssuer)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.Equal(t, "org-level-cid", clients[0].ExternalClientID)
}

// TestListClients_OrgLevelClientFromOtherOrgNotResolved is the cross-tenant
// guard: a client placed in a foreign organization must not resolve for the
// caller's sign-in. The schema also refuses placing it in the caller's project.
func TestListClients_OrgLevelClientFromOtherOrgNotResolved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	otherOrg := createOrganization(t, ctx, ti.conn, "other-org")
	otherOrgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrg, "other-org-issuer")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cross-org-usi")
	otherClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrg, otherOrgIssuer, "other-org-cid")
	foreignServer := seedMCPServerInOrg(t, ctx, ti.conn, otherOrg, "foreign-placement")
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{ID: foreignServer, OrganizationID: otherOrg})
	require.NoError(t, err)
	associateClient(t, ctx, ti.conn, otherOrg, server.ProjectID, foreignServer, otherClient)
	callerServer := seedServerOnIssuer(t, ctx, ti.conn, *authCtx.ProjectID, userIssuer, "caller-placement")
	association := testrepo.InsertMCPServerRemoteSessionClientFixtureParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, McpServerID: callerServer, RemoteSessionClientID: otherClient}
	err = testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, association)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "mcp_server_remote_session_clients_client_scope_check", pgErr.ConstraintName)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Empty(t, clients)
}

// TestGetRemoteSessionClient_OrgLevelClientHidesOtherProjectsUserSessionIssuerIds
// confirms a project-scoped read of an organization-level client reports only
// the caller's project's user_session_issuer attachments, not other projects'
// bindings on the shared client.
func TestGetRemoteSessionClient_OrgLevelClientHidesOtherProjectsUserSessionIssuerIds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "p2-issuer")
	callerUserIssuer := createUserSessionIssuer(t, ctx, ti.conn, "p2-caller-usi")
	otherProject := createProject(t, ctx, ti.conn, "p2-other-project")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "p2-other-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "p2-cid", callerUserIssuer)
	otherServer := placeClientForSignInInProject(t, ctx, ti.conn, otherProject, authCtx.ActiveOrganizationID, orgClient, otherUserIssuer)
	require.Equal(t, []uuid.UUID{orgClient}, associatedClients(t, ctx, ti.conn, otherProject, otherServer), "the foreign placement must exist to exercise project scoping")

	view, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{ID: orgClient.String()})
	require.NoError(t, err)
	require.Equal(t, []string{callerUserIssuer.String()}, view.UserSessionIssuerIds)

	q := repo.New(ti.conn)
	rows, err := q.ListRemoteSessionClientsByProjectID(ctx, repo.ListRemoteSessionClientsByProjectIDParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		RemoteSessionIssuerID: conv.ToNullUUID(orgIssuer), Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, []uuid.UUID{callerUserIssuer}, rows[0].UserSessionIssuerIds)

	filtered, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, UserSessionIssuerID: callerUserIssuer,
		RemoteSessionIssuerID: conv.ToNullUUID(orgIssuer), Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: 10,
	})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, []uuid.UUID{callerUserIssuer}, filtered[0].UserSessionIssuerIds)
}

// TestUpdateRemoteSessionClient_OrgLevelClientNotFoundFromProject enforces the
// privilege boundary: org-level clients are not mutable from the project
// surface. The project-scoped update's pre-read is project-only, so an org-level
// client id resolves to a clean not-found rather than a silent no-op update.
func TestUpdateRemoteSessionClient_OrgLevelClientNotFoundFromProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "update-404-issuer")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "update-404-cid")

	newMethod := "client_secret_post"
	_, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:                      orgClient.String(),
		TokenEndpointAuthMethod: &newMethod,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// withoutOrganizationAdmin narrows the caller to a project editor: write access
// to the project and its MCP servers, read access to the organization.
func withoutOrganizationAdmin(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	projectID := projectIDFromContext(t, ctx).String()
	return authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeOrgRead, activeOrganizationID(t, ctx)),
		authz.NewGrant(authz.ScopeProjectRead, projectID),
		authz.NewGrant(authz.ScopeProjectWrite, projectID),
		authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource),
		authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
	)
}
