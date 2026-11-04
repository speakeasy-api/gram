package authclients_test

import (
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	builtinrepo "github.com/speakeasy-api/gram/server/internal/builtinmcp/repo"
	"github.com/stretchr/testify/require"
)

func TestBuiltinStaticCredentialAttachment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	require.NoError(t, builtinmcp.EnsureServer(ctx, ti.conn, ti.organizationID, ti.projectID, builtinmcp.Slack, uuid.NullUUID{}))
	serverID := builtinmcp.ID(ti.projectID, builtinmcp.Slack)
	client, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{
		ClientType: authz.AuthClientTypeStaticCredential, Name: "Slack token",
		Headers:      []*gen.AuthClientHeaderInput{{Name: "Authorization", IsSecret: true, Value: new("Bearer example")}},
		McpServerIds: []string{serverID.String()},
	})
	require.NoError(t, err)
	require.NotNil(t, client)
	other := seedStaticClient(t, ctx, ti.conn, ti.organizationID, ti.projectID, "Other token")
	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: other.String(), McpServerID: serverID.String()})
	require.NoError(t, err)
	for _, backend := range []string{"approvals", "unknown"} {
		id := uuid.New()
		err := builtinrepo.New(ti.conn).CreateBuiltinServerFixture(ctx, builtinrepo.CreateBuiltinServerFixtureParams{ID: id, ProjectID: ti.projectID, BuiltinBackend: backend})
		require.NoError(t, err)
		_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: other.String(), McpServerID: id.String()})
		require.Error(t, err)
	}
	otherProject := seedProject(t, ctx, ti)
	require.NoError(t, builtinmcp.EnsureServer(ctx, ti.conn, ti.organizationID, otherProject, builtinmcp.Slack, uuid.NullUUID{}))
	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: other.String(), McpServerID: builtinmcp.ID(otherProject, builtinmcp.Slack).String()})
	require.Error(t, err)
	foreignOrg, foreignProject := seedOtherOrganization(t, ctx, ti.conn)
	require.NoError(t, builtinmcp.EnsureServer(ctx, ti.conn, foreignOrg, foreignProject, builtinmcp.Slack, uuid.NullUUID{}))
	_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: other.String(), McpServerID: builtinmcp.ID(foreignProject, builtinmcp.Slack).String()})
	require.Error(t, err)
}

func TestBuiltinRejectsNonStaticClients(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = withOrgGrants(t, ctx, authz.ScopeOrgRead, authz.ScopeOrgAdmin)
	require.NoError(t, builtinmcp.EnsureServer(ctx, ti.conn, ti.organizationID, ti.projectID, builtinmcp.Slack, uuid.NullUUID{}))
	serverID := builtinmcp.ID(ti.projectID, builtinmcp.Slack).String()
	personal := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "subject")
	machine := seedRegistration(t, ctx, ti.conn, ti.organizationID, ti.projectID, "self")
	cases := []*gen.CreateAuthClientPayload{
		{ClientType: authz.AuthClientTypeRemoteSession, Name: "Personal", ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(personal.String())},
		{ClientType: authz.AuthClientTypeServiceAccount, Name: "Machine", ProjectID: new(ti.projectID.String()), RemoteSessionClientID: new(machine.String())},
		{ClientType: authz.AuthClientTypePassThrough, Name: "Forward", Headers: []*gen.AuthClientHeaderInput{{Name: "X-Key", ValueFromRequestHeader: new("X-Upstream-Key")}}},
	}
	for _, payload := range cases {
		t.Run(payload.ClientType, func(t *testing.T) {
			t.Parallel()
			client, err := ti.service.CreateAuthClient(ctx, payload)
			require.NoError(t, err)
			_, err = ti.service.AttachAuthClientToMcpServer(ctx, &gen.AttachAuthClientToMcpServerPayload{ID: client.ID, McpServerID: serverID})
			require.Error(t, err)
			payload.McpServerIds = []string{serverID}
			_, err = ti.service.CreateAuthClient(ctx, payload)
			require.Error(t, err)
		})
	}
	_, err := ti.service.CreateAuthClient(ctx, &gen.CreateAuthClientPayload{ClientType: authz.AuthClientTypeEMA, Name: "EMA", McpServerIds: []string{serverID}})
	require.Error(t, err)
}
