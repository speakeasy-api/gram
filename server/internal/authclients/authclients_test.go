package authclients_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestListAuthClientsRedactsValuesAndShowsSharedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	private := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	public := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Public)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared service credential")
	associate(t, ctx, ti.conn, ti.organizationID, private, clientID)
	associate(t, ctx, ti.conn, ti.organizationID, public, clientID)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead)
	res, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)

	client := res.Items[0]
	require.Equal(t, clientID.String(), client.ID)
	require.Equal(t, authz.AuthClientTypeStaticCredential, client.ClientType)
	require.False(t, client.Personal)
	require.Len(t, client.Servers, 2)
	publicCount := 0
	for _, srv := range client.Servers {
		if srv.Public {
			publicCount++
		}
	}
	require.Equal(t, 1, publicCount)

	require.Len(t, client.Headers, 1)
	require.True(t, client.Headers[0].HasStoredValue)
	require.True(t, client.Headers[0].IsSecret)
	require.NotContains(t, fmt.Sprintf("%+v", *client.Headers[0]), "super-secret-value")

	other := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	filtered, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{McpServerID: new(other.id.String())})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)
}

func TestAuthClientReadsRequireOrgRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")

	ctx = withOrgGrants(t, ctx)
	_, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetAuthClient(ctx, &gen.GetAuthClientPayload{ID: clientID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestAuthClientsAreTenantScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherOrg, otherProject := seedOtherOrganization(t, ctx, ti.conn)
	foreign := seedStaticClient(t, ctx, ti.conn, otherOrg, otherProject, "Foreign")

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	res, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{})
	require.NoError(t, err)
	require.Empty(t, res.Items)

	_, err = ti.service.GetAuthClient(ctx, &gen.GetAuthClientPayload{ID: foreign.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: foreign.String(), Enabled: new(false)})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateAuthClientAuditsEachChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")

	readOnly := withOrgGrants(t, ctx, authz.ScopeOrgRead)
	_, err := ti.service.UpdateAuthClient(readOnly, &gen.UpdateAuthClientPayload{ID: clientID.String(), Enabled: new(false)})
	requireOopsCode(t, err, oops.CodeForbidden)

	admin := withOrgGrants(t, ctx, authz.ScopeOrgAdmin)
	disableBefore := auditCount(t, ti, audit.ActionAuthClientDisable)
	renameBefore := auditCount(t, ti, audit.ActionAuthClientUpdate)

	updated, err := ti.service.UpdateAuthClient(admin, &gen.UpdateAuthClientPayload{ID: clientID.String(), Name: new("Renamed"), Enabled: new(false)})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.False(t, updated.Enabled)
	require.Equal(t, disableBefore+1, auditCount(t, ti, audit.ActionAuthClientDisable))
	require.Equal(t, renameBefore+1, auditCount(t, ti, audit.ActionAuthClientUpdate))

	_, err = ti.service.UpdateAuthClient(admin, &gen.UpdateAuthClientPayload{ID: clientID.String(), Enabled: new(false)})
	require.NoError(t, err)
	require.Equal(t, disableBefore+1, auditCount(t, ti, audit.ActionAuthClientDisable), "a no-op update records nothing")

	enableBefore := auditCount(t, ti, audit.ActionAuthClientEnable)
	_, err = ti.service.UpdateAuthClient(admin, &gen.UpdateAuthClientPayload{ID: clientID.String(), Enabled: new(true)})
	require.NoError(t, err)
	require.Equal(t, enableBefore+1, auditCount(t, ti, audit.ActionAuthClientEnable))
}

func TestDetachAuthClientKeepsOtherServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	second := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Shared")
	associate(t, ctx, ti.conn, ti.organizationID, first, clientID)
	associate(t, ctx, ti.conn, ti.organizationID, second, clientID)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgAdmin)
	before := auditCount(t, ti, audit.ActionAuthClientDetachMcpServer)

	res, err := ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID.String(), McpServerID: first.id.String()})
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	require.Equal(t, second.id.String(), res.Servers[0].ID)
	require.Equal(t, before+1, auditCount(t, ti, audit.ActionAuthClientDetachMcpServer))

	_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID.String(), McpServerID: first.id.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestMcpServerAuthenticationStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Public)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	status := func() *types.McpServerAuthentication {
		t.Helper()
		got, err := ti.service.GetMcpServerAuthentication(ctx, &gen.GetMcpServerAuthenticationPayload{McpServerID: server.id.String()})
		require.NoError(t, err)
		return got
	}

	got := status()
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status, "a server with no auth client and no upstream credentials needs none")
	require.False(t, got.RequiresUpstreamAuth)
	require.True(t, got.Public)

	first := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "First")
	second := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Second")
	associate(t, ctx, ti.conn, ti.organizationID, server, first)
	associate(t, ctx, ti.conn, ti.organizationID, server, second)
	got = status()
	require.Equal(t, authclients.StatusConfigured, got.Status)
	require.True(t, got.RequiresUpstreamAuth)
	require.Len(t, got.Clients, 2)

	_, err := ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: first.String(), Enabled: new(false)})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusConfigured, status().Status, "one enabled client of several is enough")

	_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: second.String(), Enabled: new(false)})
	require.NoError(t, err)
	got = status()
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status, "with every client disabled the server needs none")
	require.False(t, got.RequiresUpstreamAuth)

	_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: second.String(), Enabled: new(true)})
	require.NoError(t, err, "enabling a client is never refused by a stored requirement")
	require.Equal(t, authclients.StatusConfigured, status().Status)

	_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: second.String(), McpServerID: server.id.String()})
	require.NoError(t, err)
	got = status()
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status, "a detached client no longer counts")
	require.Len(t, got.Clients, 1)
}

func TestMcpServerAuthenticationStatusIgnoresTheStoredRequirement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	queries := testrepo.New(ti.conn)

	require.NoError(t, queries.SetMCPServerRequiresUpstreamAuthFixture(ctx, testrepo.SetMCPServerRequiresUpstreamAuthFixtureParams{ID: server.id, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
	got, err := ti.service.GetMcpServerAuthentication(ctx, &gen.GetMcpServerAuthenticationPayload{McpServerID: server.id.String()})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status)
	require.False(t, got.RequiresUpstreamAuth)

	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")
	associate(t, ctx, ti.conn, ti.organizationID, server, clientID)
	require.NoError(t, queries.SetMCPServerRequiresUpstreamAuthFixture(ctx, testrepo.SetMCPServerRequiresUpstreamAuthFixtureParams{ID: server.id, RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}}))
	got, err = ti.service.GetMcpServerAuthentication(ctx, &gen.GetMcpServerAuthenticationPayload{McpServerID: server.id.String()})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusConfigured, got.Status)
	require.True(t, got.RequiresUpstreamAuth)
}

// Upstream credentials that no auth client records yet are incomplete setup:
// the migration has not reached the server, which is never read as needing
// no authentication.
func TestMcpServerAuthenticationStatusTellsPendingSetupFromNoAuth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	require.NoError(t, testrepo.New(ti.conn).CreateRemoteMCPCredentialHeaderFixture(ctx, testrepo.CreateRemoteMCPCredentialHeaderFixtureParams{RemoteMcpServerID: server.remoteID, Value: conv.ToPGText("sealed")}))

	got, err := ti.service.GetMcpServerAuthentication(ctx, &gen.GetMcpServerAuthenticationPayload{McpServerID: server.id.String()})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusSetupIncomplete, got.Status)
	require.False(t, got.RequiresUpstreamAuth)

	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")
	associate(t, ctx, ti.conn, ti.organizationID, server, clientID)
	_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID.String(), McpServerID: server.id.String()})
	require.NoError(t, err)
	got, err = ti.service.GetMcpServerAuthentication(ctx, &gen.GetMcpServerAuthenticationPayload{McpServerID: server.id.String()})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status, "an administrator's detach is a decision, not a pending migration")
}

func TestSetMcpServerUpstreamAuthRequirementIgnoresTheRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)

	got, err := ti.service.SetMcpServerUpstreamAuthRequirement(ctx, &gen.SetMcpServerUpstreamAuthRequirementPayload{McpServerID: server.id.String(), RequiresUpstreamAuth: true})
	require.NoError(t, err)
	require.Equal(t, authclients.StatusNoAuthRequired, got.Status)
	require.False(t, got.RequiresUpstreamAuth)

	clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")
	associate(t, ctx, ti.conn, ti.organizationID, server, clientID)
	got, err = ti.service.SetMcpServerUpstreamAuthRequirement(ctx, &gen.SetMcpServerUpstreamAuthRequirementPayload{McpServerID: server.id.String(), RequiresUpstreamAuth: false})
	require.NoError(t, err, "declaring no auth with an enabled client is no longer refused")
	require.Equal(t, authclients.StatusConfigured, got.Status)
	require.True(t, got.RequiresUpstreamAuth)

	stored, err := testrepo.New(ti.conn).GetMCPServerRequiresUpstreamAuthFixture(ctx, server.id)
	require.NoError(t, err)
	require.False(t, stored.Valid, "the request writes nothing")
}

func TestSetMcpServerUpstreamAuthRequirementRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)

	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead)
	_, err := ti.service.SetMcpServerUpstreamAuthRequirement(ctx, &gen.SetMcpServerUpstreamAuthRequirementPayload{McpServerID: server.id.String(), RequiresUpstreamAuth: true})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func auditCount(t *testing.T, ti *testInstance, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, action)
	require.NoError(t, err)
	return count
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// Legacy API keys skip RBAC, so whether bound to a project or not they could
// otherwise read and manage auth clients without org:read or org:admin.
func TestLegacyKeysCannotManageAuthClients(t *testing.T) {
	t.Parallel()

	for name, projectBound := range map[string]bool{"project-bound": true, "organization-wide": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			clientID := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Credential")
			server := seedServer(t, ctx, ti.conn, ti.projectID, visibility.Private)
			associate(t, ctx, ti.conn, ti.organizationID, server, clientID)

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			keyCtx := *authCtx
			keyCtx.APIKeyID = uuid.NewString()
			keyCtx.ProjectID = nil
			if projectBound {
				keyCtx.ProjectID = new(ti.projectID)
			}
			ctx = contextvalues.WithLegacyAPIKeyAuthorization(ctx, &keyCtx)

			_, err := ti.service.ListAuthClients(ctx, &gen.ListAuthClientsPayload{Limit: 50})
			requireOopsCode(t, err, oops.CodeForbidden)

			_, err = ti.service.GetAuthClient(ctx, &gen.GetAuthClientPayload{ID: clientID.String()})
			requireOopsCode(t, err, oops.CodeForbidden)

			_, err = ti.service.UpdateAuthClient(ctx, &gen.UpdateAuthClientPayload{ID: clientID.String(), Enabled: new(false)})
			requireOopsCode(t, err, oops.CodeForbidden)

			_, err = ti.service.DetachAuthClientFromMcpServer(ctx, &gen.DetachAuthClientFromMcpServerPayload{ID: clientID.String(), McpServerID: server.id.String()})
			requireOopsCode(t, err, oops.CodeForbidden)

			_, err = ti.service.SetMcpServerUpstreamAuthRequirement(ctx, &gen.SetMcpServerUpstreamAuthRequirementPayload{McpServerID: server.id.String(), RequiresUpstreamAuth: false})
			requireOopsCode(t, err, oops.CodeForbidden)
		})
	}
}
