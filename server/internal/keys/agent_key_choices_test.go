package keys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestKeysService_AgentKeyRecordsCredentialChoices(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)
	organizationID := testAuthContext(t, ctx).ActiveOrganizationID

	fixtures := testrepo.New(ti.conn)
	remoteID := uuid.New()
	require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{ID: remoteID, ProjectID: projectID, Url: "https://upstream.example.com/" + remoteID.String()}))
	serverID := uuid.New()
	require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: serverID, ProjectID: projectID, Slug: conv.ToPGText("srv-" + serverID.String()[:8]), RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: "private",
	}))
	clients := authclientbackfill.New(ti.conn)
	offer := func(clientType string) uuid.UUID {
		id, err := clients.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ClientType: clientType, Source: "admin"})
		require.NoError(t, err)
		require.NoError(t, clients.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: organizationID, McpServerID: serverID, AuthClientID: id, Source: "admin"}))
		return id
	}
	shared := offer(authz.AuthClientTypeStaticCredential)
	personal := offer(authz.AuthClientTypeRemoteSession)

	refused := map[string]*gen.AgentKeyAuthClientChoice{
		"a personal connection":            {McpServerID: serverID.String(), AuthClientID: new(personal.String())},
		"no-auth on a server with clients": {McpServerID: serverID.String()},
		"a client the server lacks":        {McpServerID: serverID.String(), AuthClientID: new(uuid.NewString())},
	}
	for name, choice := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload := agentKeyPayload(agentID, projectID)
			payload.Name = "refused " + name
			payload.AuthClientChoices = []*gen.AgentKeyAuthClientChoice{choice}
			_, err := ti.service.CreateKey(ctx, payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}

	// A second server needing no upstream authentication takes a no-auth choice.
	noAuthRemote := uuid.New()
	require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{ID: noAuthRemote, ProjectID: projectID, Url: "https://open.example.com/" + noAuthRemote.String()}))
	noAuthServer := uuid.New()
	require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: noAuthServer, ProjectID: projectID, Slug: conv.ToPGText("open-" + noAuthServer.String()[:8]), RemoteMcpServerID: uuid.NullUUID{UUID: noAuthRemote, Valid: true}, Visibility: "private",
	}))
	require.NoError(t, clients.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: noAuthServer, RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}}))

	// One refused choice refuses the whole key, so the valid one before it is
	// not recorded either and the name stays free.
	partial := agentKeyPayload(agentID, projectID)
	partial.Name = "all or nothing"
	partial.AuthClientChoices = []*gen.AgentKeyAuthClientChoice{{McpServerID: noAuthServer.String()}, refused["a personal connection"]}
	_, err := ti.service.CreateKey(ctx, partial)
	requireOopsCode(t, err, oops.CodeBadRequest)
	partial.AuthClientChoices = partial.AuthClientChoices[:1]
	open, err := ti.service.CreateKey(ctx, partial)
	require.NoError(t, err, "the refused key left nothing behind")
	openChoice, err := clients.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: uuid.MustParse(open.ID), McpServerID: noAuthServer})
	require.NoError(t, err)
	require.True(t, openChoice.NoAuth, "a server needing no authentication takes a no-auth choice")

	payload := agentKeyPayload(agentID, projectID)
	payload.AuthClientChoices = []*gen.AgentKeyAuthClientChoice{{McpServerID: serverID.String(), AuthClientID: new(shared.String())}}
	created, err := ti.service.CreateKey(ctx, payload)
	require.NoError(t, err)

	choice, err := clients.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: uuid.MustParse(created.ID), McpServerID: serverID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: shared, Valid: true}, choice.AuthClientID)

	rotated, err := ti.service.RotateKey(ctx, &gen.RotateKeyPayload{ID: created.ID, Name: created.Name, DelegatedGrantsVersion: 1, RequestedGrants: payload.RequestedGrants})
	require.NoError(t, err)
	choice, err = clients.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: uuid.MustParse(rotated.ID), McpServerID: serverID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: shared, Valid: true}, choice.AuthClientID, "rotation keeps the key's choices")

	ordinary := &gen.CreateKeyPayload{Name: "ordinary", AuthClientChoices: payload.AuthClientChoices}
	_, err = ti.service.CreateKey(ctx, ordinary)
	requireOopsCode(t, err, oops.CodeBadRequest)
}
