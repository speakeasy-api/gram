package remotesessions_test

import (
	"context"
	"testing"
	"time"

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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
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

// TestAttachUserSessionIssuer_OrgLevelClientConflictsWithProjectClient proves
// the single-client invariant now spans scopes: a project-scoped client and an
// org-level client cannot both bind the same remote issuer to one
// user_session_issuer. The guard counts the already-bound project client when
// the org-level client is attached.
func TestAttachUserSessionIssuer_OrgLevelClientConflictsWithProjectClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "guard-issuer")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "guard-usi")
	server := seedServerOnIssuer(t, ctx, ti.conn, *authCtx.ProjectID, userIssuer, "guard-project-server")
	projectClient := createRemoteClient(t, ctx, ti, orgIssuer.String(), userIssuer.String(), "guard-project-cid")

	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "guard-org-cid")

	_, err := ti.service.AttachLegacyIssuerForTest(ctx, &remotesessions.LegacyIssuerBindingForTest{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, []uuid.UUID{uuid.MustParse(projectClient)}, serverAssociatedClients(t, ctx, ti, server))
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

// TestDetachUserSessionIssuer_RejectsOtherProjectsBindingOnOrgLevelClient
// enforces the privilege boundary on detach: an org-level client can be bound to
// user_session_issuers across projects in the org, but a project admin must not
// be able to detach a binding on another project's user_session_issuer even
// though it can resolve the shared org-level client.
func TestDetachUserSessionIssuer_RejectsOtherProjectsBindingOnOrgLevelClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "detach-boundary-issuer")

	// A second project in the same org, with its own user_session_issuer that the
	// org-level client is bound to.
	otherProject := createProject(t, ctx, ti.conn, "detach-boundary-other-project")
	otherUserIssuer := createUserSessionIssuerInProject(t, ctx, ti.conn, otherProject, "detach-boundary-other-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "detach-boundary-cid")
	placeClientForSignInInProject(t, ctx, ti.conn, otherProject, authCtx.ActiveOrganizationID, orgClient, otherUserIssuer)

	// The caller (the auth context's own project) must not be able to detach the
	// other project's binding.
	_, err := ti.service.DetachLegacyIssuerForTest(ctx, &remotesessions.LegacyIssuerBindingForTest{
		ID:                  orgClient.String(),
		UserSessionIssuerID: otherUserIssuer.String(),
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
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

// An organization-level client on an organization-level user session issuer
// is shared by every project's servers, so a project editor may not change it.
func TestAttachUserSessionIssuer_RefusesOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "orgwide-attach-issuer")
	userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "orgwide-attach-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "orgwide-attach-cid")

	_, err := ti.service.AttachLegacyIssuerForTest(withoutOrganizationAdmin(t, ctx), &remotesessions.LegacyIssuerBindingForTest{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	require.Zero(t, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, orgClient, userIssuer))
}

func TestDetachUserSessionIssuer_RefusesOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "orgwide-detach-issuer")
	userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "orgwide-detach-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "orgwide-detach-cid")
	ensureAttachmentParent(t, ctx, ti.conn, orgClient, userIssuer)

	_, err := ti.service.DetachLegacyIssuerForTest(withoutOrganizationAdmin(t, ctx), &remotesessions.LegacyIssuerBindingForTest{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, orgClient, userIssuer))
}

// The pre-lock binding snapshot must not turn a concurrent org-wide change
// into an authorized project-scoped mutation.
func TestClientAttachment_OrgWideGuardUsesLockedBinding(t *testing.T) {
	t.Parallel()

	for _, attach := range []bool{true, false} {
		name := "detach"
		if attach {
			name = "attach"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "concurrent-org-provider")
			userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "concurrent-org-issuer")
			orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, activeOrganizationID(t, ctx), orgIssuer, "concurrent-org-client")
			if attach {
				require.NoError(t, repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: orgClient, UserSessionIssuerID: userIssuer}))
			}
			tx := testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, usersessionsrepo.New(tx).LockUserSessionIssuerForOwnerBinding(ctx, userIssuer))
			_, err := repo.New(tx).LockProjectUserIssuerForDetach(ctx, repo.LockProjectUserIssuerForDetachParams{ID: userIssuer, ProjectID: projectIDFromContext(t, ctx), OrganizationID: activeOrganizationID(t, ctx)})
			require.NoError(t, err)
			done := make(chan error, 1)
			editorCtx := withoutOrganizationAdmin(t, ctx)
			go func() {
				if attach {
					_, err := ti.service.AttachLegacyIssuerForTest(editorCtx, &remotesessions.LegacyIssuerBindingForTest{ID: orgClient.String(), UserSessionIssuerID: userIssuer.String()})
					done <- err
				} else {
					_, err := ti.service.DetachLegacyIssuerForTest(editorCtx, &remotesessions.LegacyIssuerBindingForTest{ID: orgClient.String(), UserSessionIssuerID: userIssuer.String()})
					done <- err
				}
			}()
			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), 1)
			// Simulate an organization-authorized mutation while the project request
			// waits for the issuer lock, after it has read the old binding state.
			if attach {
				removed, err := testrepo.New(tx).DetachRemoteSessionClientFromUserSessionIssuer(ctx, testrepo.DetachRemoteSessionClientFromUserSessionIssuerParams{RemoteSessionClientID: orgClient, UserSessionIssuerID: userIssuer})
				require.NoError(t, err)
				require.Equal(t, int64(1), removed)
			} else {
				require.NoError(t, repo.New(tx).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: orgClient, UserSessionIssuerID: userIssuer}))
			}
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				requireOopsCode(t, err, oops.CodeForbidden)
				require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
			case <-time.After(30 * time.Second):
				t.Fatal("binding change did not complete after releasing issuer lock")
			}
			want := 1
			if attach {
				want = 0
			}
			require.Equal(t, want, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, orgClient, userIssuer))
		})
	}
}

func TestListClientsBySignIn_IncludesOrganizationGatewayPlacement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	project, org := projectIDFromContext(t, ctx), activeOrganizationID(t, ctx)
	gatewaySignIn := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "list-org-gateway")
	memberSignIn := createUserSessionIssuer(t, ctx, ti.conn, "list-org-member")
	provider := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, org, "list-org-provider")
	registration := seedOrgLevelRemoteClient(t, ctx, ti.conn, org, provider, "list-org-registration", memberSignIn)
	q := repo.New(ti.conn)
	params := repo.ListRemoteSessionClientsByProjectIDForUserSessionIssuerParams{
		ProjectID: project, OrganizationID: org, UserSessionIssuerID: gatewaySignIn,
		RemoteSessionIssuerID: conv.ToNullUUID(provider), Cursor: uuid.NullUUID{}, LimitValue: 10,
	}
	rows, err := q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, registration, rows[0].RemoteSessionClient.ID)
	seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "list-org-other-gateway")
	rows, err = q.ListRemoteSessionClientsByProjectIDForUserSessionIssuer(ctx, params)
	require.NoError(t, err)
	require.Empty(t, rows, "an ambiguous organization issuer must not acquire placements")
}
