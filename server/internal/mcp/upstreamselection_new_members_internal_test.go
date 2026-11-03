package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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
	f.serverID = noAuth
	require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{Enabled: false, ID: staticClient()}))
	join(noAuth)

	// An upstream OAuth client no auth client records yet.
	pending, _ := f.server()
	f.serverID = pending
	f.attach()
	join(pending)

	choices := &memberChoices{
		selector:     &upstreamSelector{service: f.service, organizationID: f.orgID, userSessionIssuerID: issuerID, credentialKind: upstreamclients.CredentialKindUserSession, credentialID: credentialID},
		subject:      user,
		enforced:     true,
		metaServerID: validUUID(gatewayID),
		byServer:     map[uuid.UUID]*memberTokens{},
	}
	members := []metaMember{member(considered), member(single), member(ambiguous), member(noAuth), member(pending)}
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
	_, err = selection(pending)
	require.Error(t, err, "a new member whose upstream credentials no auth client records yet is never recorded as no-auth")
	_, err = selection(considered)
	require.Error(t, err, "a member present at issuance is not a new addition")

	// A recorded choice is never replaced, even once the member offers more.
	f.serverID = single
	staticClient()
	choices.recordNewMemberChoices(ctx, testenv.NewLogger(t), []metaMember{member(single)}, map[uuid.UUID]authClientResolution{})
	row, err = selection(single)
	require.NoError(t, err)
	require.Equal(t, validUUID(singleClient), row.AuthClientID)

	// The recorded no-auth choice serves on every request, though the server
	// still has a disabled header client attached.
	for range 2 {
		served := []metaMember{member(noAuth)}
		choices.apply(ctx, testenv.NewLogger(t), served, true)
		require.NoError(t, served[0].upstreamUnavailable)
		require.True(t, served[0].upstreamNoAuth)
	}
}

func TestAgentKeySelectorReadsOnlyTheKeysOwnChoices(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_agent_key_selector")
	require.NoError(t, err)
	ctx := t.Context()
	f := newAuthClientFixture(t, pool)
	clientID := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	keyID := uuid.New()
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	keyCtx := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String()}, agent, contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})

	selector, err := f.service.agentKeySelectorFor(keyCtx, endpoint)
	require.NoError(t, err)
	require.Nil(t, selector, "a key with no choice for the server keeps the issuer-derived path")

	// A sibling key's choice for this server, and this key's choice for
	// another server, are not this key's choice here.
	otherServer, _ := f.server()
	for _, other := range []upstreamclientsrepo.RecordAuthClientChoiceParams{
		{CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: uuid.New(), Source: upstreamclients.SelectionSourceIssuance, OrganizationID: f.orgID, AuthClientID: validUUID(clientID), McpServerID: f.serverID},
		{CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance, OrganizationID: f.orgID, AuthClientID: uuid.NullUUID{}, McpServerID: otherServer},
	} {
		_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(ctx, other)
		require.NoError(t, err)
	}
	selector, err = f.service.agentKeySelectorFor(keyCtx, endpoint)
	require.NoError(t, err)
	require.Nil(t, selector, "other keys' choices and this key's other servers do not apply")

	_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance,
		OrganizationID: f.orgID, AuthClientID: validUUID(clientID), McpServerID: f.serverID,
	})
	require.NoError(t, err)

	selector, err = f.service.agentKeySelectorFor(keyCtx, endpoint)
	require.NoError(t, err)
	require.NotNil(t, selector)
	require.Equal(t, upstreamclients.CredentialKindAPIKey, selector.credentialKind)
	require.Equal(t, keyID, selector.credentialID)

	res, err := selector.resolveAuthClient(ctx, f.serverID)
	require.NoError(t, err)
	require.Equal(t, clientID, res.clientID)

	legacyCtx := contextvalues.WithLegacyAPIKeyAuthorization(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String(), UserID: "user_1"})
	selector, err = f.service.agentKeySelectorFor(legacyCtx, endpoint)
	require.NoError(t, err)
	require.Nil(t, selector, "only agent keys are served through key choices")
}

func TestAgentKeyChoiceServesAServerWithoutAnIssuerGate(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_agent_key_ungated")
	require.NoError(t, err)
	ctx := t.Context()
	f := newAuthClientFixture(t, pool)
	f.service.authClientPermitter = fixedPermitter(true)
	clientID := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(clientID, "X-Api-Key", false, false, text("chosen-value"), pgtype.Text{})
	keyID := uuid.New()
	_, err = upstreamclientsrepo.New(pool).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance,
		OrganizationID: f.orgID, AuthClientID: validUUID(clientID), McpServerID: f.serverID,
	})
	require.NoError(t, err)

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	keyCtx := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String()}, agent, contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})
	endpoint := &mcpendpointsrepo.McpEndpoint{ProjectID: f.projectID}
	server := &mcpserversrepo.McpServer{ID: f.serverID, ProjectID: f.projectID, RemoteMcpServerID: validUUID(f.remoteID)}
	sentinel := []remotemcp.BuildOption{remotemcp.WithAnonymousCaller()}
	serve := func(ctx context.Context, preserved bool) []remotemcp.BuildOption {
		t.Helper()
		upstream := remotesessions.UpstreamToken{Token: "existing-token"}
		got, options, err := f.service.agentKeyUngatedUpstream(ctx, httptest.NewRecorder(), testenv.NewLogger(t), "https://mcp.example.com", endpoint, server, f.orgID, upstream, sentinel)
		require.NoError(t, err)
		if preserved {
			require.Equal(t, upstream, got)
			require.Same(t, &sentinel[0], &options[0], "existing options must be preserved")
		}
		return options
	}

	require.Len(t, serve(keyCtx, true), 1, "before enforcement the server's own headers serve the key")
	f.enforce()
	require.Len(t, serve(keyCtx, false), 1, "the key's chosen credential replaces the server's headers")
	require.Len(t, serve(ctx, true), 1, "a caller with no agent key keeps the server's headers")
	omittedChoiceCtx := contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: uuid.NewString()}, agent, contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})
	require.Len(t, serve(omittedChoiceCtx, true), 1, "an agent key with no recorded choice keeps legacy headers")
	server.UserSessionIssuerID = validUUID(uuid.New())
	require.Len(t, serve(keyCtx, true), 1, "the issuer-gated path keeps its already-resolved options")
}
