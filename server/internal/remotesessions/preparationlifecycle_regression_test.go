package remotesessions_test

import (
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"testing"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestPreparationLifecycle_GlobalMutationsDoNotProbeTenantBindings(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	admin := withAdmin(t, ctx)
	_, err = ti.service.UpdateGlobalClient(admin, &adminrsgen.UpdateGlobalClientPayload{ID: in.ClientID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	require.NoError(t, ti.service.DeleteGlobalClient(admin, &adminrsgen.DeleteGlobalClientPayload{ID: in.ClientID.String()}))
	_, err = ti.service.UpdateGlobalIssuer(admin, &adminrsgen.UpdateGlobalIssuerPayload{ID: in.RemoteSessionIssuerID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
	// A legitimate mutation still sees the binding and is blocked.
	requireOopsCode(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()}), oops.CodeConflict)
}

func TestPreparationLifecycle_ProjectDeleteDoesNotProbeSiblingBindings(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sibling := createProject(t, ctx, ti.conn, "lifecycle-sibling")
	siblingAuth := *auth
	siblingAuth.ProjectID = &sibling
	siblingCtx := contextvalues.SetAuthContext(ctx, &siblingAuth)
	require.NoError(t, ti.service.DeleteRemoteSessionClient(siblingCtx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()}))
	requireOopsCode(t, ti.service.DeleteRemoteSessionClient(ctx, &clientsgen.DeleteRemoteSessionClientPayload{ID: in.ClientID.String()}), oops.CodeConflict)
}

func TestPreparationLifecycle_DetachAbsentJoinIgnoresEMABinding(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	payload := &clientsgen.DetachUserSessionIssuerPayload{ID: in.ClientID.String(), UserSessionIssuerID: in.UserSessionIssuerID.String()}
	_, err = ti.service.DetachUserSessionIssuer(ctx, payload)
	require.NoError(t, err, "absent interactive join is a no-op despite the EMA binding")
	err = repo.New(ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: in.ClientID, UserSessionIssuerID: in.UserSessionIssuerID})
	require.NoError(t, err)
	_, err = ti.service.DetachUserSessionIssuer(ctx, payload)
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, 1, countRemoteSessionClientUserSessionIssuerBindings(t, ctx, ti.conn, in.ClientID, in.UserSessionIssuerID), "blocked removal rolls back the join")
}

func TestPreparationLifecycle_OrganizationDetachAbsentJoinIgnoresEMABinding(t *testing.T) {
	t.Parallel()
	ctx, ti, in := preparationFixture(t)
	preparationRecordGrants(t, ctx, ti, in.ClientID, []string{preparationJWTGrant})
	result, err := ti.service.PrepareIdentityChaining(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "ready", result.State)
	auth, _ := contextvalues.GetAuthContext(ctx)
	serverID := seedMCPServerInOrg(t, ctx, ti.conn, auth.ActiveOrganizationID, "ema-absent-detach")
	err = ti.service.RemoveClientFromMcpServer(ctx, &orgclientsgen.RemoveClientFromMcpServerPayload{ClientID: in.ClientID.String(), McpServerID: serverID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}
