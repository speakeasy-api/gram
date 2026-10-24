package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRecordNewMemberChoices(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_new_member_choices")
	require.NoError(t, err)
	ctx := t.Context()
	f := newAuthClientFixture(t, pool)
	user := urn.NewUserSubject("user_1")

	issuerID := uuid.New()
	gateways := selectionbackfill.New(pool)
	require.NoError(t, gateways.SeedUserSessionIssuerFixture(ctx, selectionbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, Slug: "usi-" + issuerID.String()[:8]}))
	gatewayID := uuid.New()
	require.NoError(t, gateways.SeedMetaMCPServerFixture(ctx, selectionbackfill.SeedMetaMCPServerFixtureParams{ID: gatewayID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "gateway", UserSessionIssuerID: validUUID(issuerID)}))
	join := func(serverID uuid.UUID) {
		require.NoError(t, gateways.SeedMetaMCPServerMemberFixture(ctx, selectionbackfill.SeedMetaMCPServerMemberFixtureParams{ProjectID: f.projectID, MetaMcpServerID: gatewayID, McpServerID: serverID}))
	}
	// staticClient gives the fixture's current server one ready shared client.
	staticClient := func() uuid.UUID {
		id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(id, "X-Api-Key", false, false, text("plain-value"), pgtype.Text{})
		return id
	}
	member := func(serverID uuid.UUID) metaMember {
		return metaMember{serverID: serverID, projectID: f.projectID, slug: "s-" + serverID.String()[:8], backend: metaMemberBackendProxied}
	}

	// Present before the credential was issued: considered then.
	considered := f.serverID
	staticClient()
	join(considered)

	credentialID := uuid.New()
	require.NoError(t, f.q.SeedUserSessionFixture(ctx, authclientbackfill.SeedUserSessionFixtureParams{
		ProjectID: validUUID(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, UserSessionIssuerID: issuerID,
		SubjectUrn: user, Jti: "jti-" + credentialID.String(), CredentialID: validUUID(credentialID),
	}))

	single, _ := f.server()
	f.serverID = single
	singleClient := staticClient()
	join(single)

	ambiguous, _ := f.server()
	f.serverID = ambiguous
	staticClient()
	staticClient()
	join(ambiguous)

	noAuth, _ := f.server()
	require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(ctx, authclientbackfill.SetRequiresUpstreamAuthFixtureParams{ID: noAuth, RequiresUpstreamAuth: pgtype.Bool{Bool: false, Valid: true}}))
	join(noAuth)

	choices := &memberChoices{
		selector:     &upstreamSelector{service: f.service, organizationID: f.orgID, userSessionIssuerID: issuerID, credentialKind: upstreamclients.CredentialKindUserSession, credentialID: credentialID},
		subject:      user,
		enforced:     true,
		metaServerID: validUUID(gatewayID),
		byServer:     map[uuid.UUID]*memberTokens{},
	}
	members := []metaMember{member(considered), member(single), member(ambiguous), member(noAuth)}
	require.True(t, choices.recordNewMemberChoices(ctx, testenv.NewLogger(t), members, map[uuid.UUID]authClientResolution{}))

	selection := func(serverID uuid.UUID) (authclientbackfill.GetSelectionFixtureRow, error) {
		return f.q.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: credentialID, McpServerID: serverID})
	}
	row, err := selection(single)
	require.NoError(t, err)
	require.Equal(t, validUUID(singleClient), row.AuthClientID, "a new member with one ready client records it")
	require.Equal(t, upstreamclients.SelectionSourceNewMember, row.Source)

	row, err = selection(noAuth)
	require.NoError(t, err)
	require.True(t, row.NoAuth, "a new member needing no authentication records no-auth")
	require.Equal(t, upstreamclients.SelectionSourceNewMember, row.Source)

	_, err = selection(ambiguous)
	require.Error(t, err, "a new member with several ready clients is left for the person to choose")
	_, err = selection(considered)
	require.Error(t, err, "a member present at issuance is not a new addition")

	// A recorded choice is never replaced, even once the member offers more.
	f.serverID = single
	staticClient()
	choices.recordNewMemberChoices(ctx, testenv.NewLogger(t), []metaMember{member(single)}, map[uuid.UUID]authClientResolution{})
	row, err = selection(single)
	require.NoError(t, err)
	require.Equal(t, validUUID(singleClient), row.AuthClientID)
}
