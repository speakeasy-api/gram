package remotesessions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
// guard: an org-level client owned by a different organization, even when
// (defensively) linked to this project's user_session_issuer, must not resolve
// for this org because the org-OR predicate matches only the caller's org.
func TestListClients_OrgLevelClientFromOtherOrgNotResolved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	otherOrg := createOrganization(t, ctx, ti.conn, "other-org")
	otherOrgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrg, "other-org-issuer")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cross-org-usi")
	seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrg, otherOrgIssuer, "other-org-cid", userIssuer)

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
	createRemoteClient(t, ctx, ti, orgIssuer.String(), userIssuer.String(), "guard-project-cid")

	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "guard-org-cid")

	_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
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
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "p2-cid", callerUserIssuer, otherUserIssuer)

	view, err := ti.service.GetRemoteSessionClient(ctx, &clientsgen.GetRemoteSessionClientPayload{ID: orgClient.String()})
	require.NoError(t, err)
	require.Equal(t, []string{callerUserIssuer.String()}, view.UserSessionIssuerIds)
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
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "detach-boundary-cid", otherUserIssuer)

	// The caller (the auth context's own project) must not be able to detach the
	// other project's binding.
	_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
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

// An organization-level client on an organization-level user session issuer
// is shared by every project's servers, so a project caller may not change it.
func TestAttachUserSessionIssuer_RefusesOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "orgwide-attach-issuer")
	userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "orgwide-attach-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "orgwide-attach-cid")

	_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	requireClientBound(t, ctx, ti, orgClient, userIssuer, false)
}

func TestDetachUserSessionIssuer_RefusesOrgClientOnOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "orgwide-detach-issuer")
	userIssuer := seedOrganizationTierUserSessionIssuer(t, ctx, ti.conn, "orgwide-detach-usi")
	orgClient := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, orgIssuer, "orgwide-detach-cid", userIssuer)

	_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{
		ID:                  orgClient.String(),
		UserSessionIssuerID: userIssuer.String(),
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
	requireClientBound(t, ctx, ti, orgClient, userIssuer, true)
}

// The pre-lock binding snapshot must not turn a concurrent org-wide change
// into an authorized project-scoped mutation.
func TestClientAttachment_OrgWideGuardUsesLockedBinding(t *testing.T) {
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
			go func() {
				if attach {
					_, err := ti.service.AttachUserSessionIssuer(ctx, &clientsgen.AttachUserSessionIssuerPayload{ID: orgClient.String(), UserSessionIssuerID: userIssuer.String()})
					done <- err
				} else {
					_, err := ti.service.DetachUserSessionIssuer(ctx, &clientsgen.DetachUserSessionIssuerPayload{ID: orgClient.String(), UserSessionIssuerID: userIssuer.String()})
					done <- err
				}
			}()
			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), 1)
			// Simulate an organization-authorized mutation while the project request
			// waits for the issuer lock, after it has read the old binding state.
			if attach {
				_, err := tx.Exec(ctx, "DELETE FROM remote_session_client_user_session_issuers WHERE remote_session_client_id = $1 AND user_session_issuer_id = $2", orgClient, userIssuer)
				require.NoError(t, err)
			} else {
				require.NoError(t, repo.New(tx).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: orgClient, UserSessionIssuerID: userIssuer}))
			}
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				requireOopsCode(t, err, oops.CodeConflict)
				require.ErrorContains(t, err, remotesessions.OrgWideBindingMessage)
			case <-time.After(30 * time.Second):
				t.Fatal("binding change did not complete after releasing issuer lock")
			}
			requireClientBound(t, ctx, ti, orgClient, userIssuer, !attach)
		})
	}
}
