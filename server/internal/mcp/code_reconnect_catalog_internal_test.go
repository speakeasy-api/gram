package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/selectionbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestReconnectCatalogChanges(t *testing.T) {
	t.Parallel()
	old := &connectionCatalog{Tools: []*toolListEntry{{Name: "source--removed"}, {Name: "source--changed", Description: "old"}, {Name: "source--same"}}}
	var inventory consentToolInventory
	for _, tool := range []*toolListEntry{{Name: "source--added"}, {Name: "source--changed", Description: "new"}, {Name: "source--same"}} {
		inventory.Tools = append(inventory.Tools, consentInventoryTool{Name: tool.Name, Definition: tool, DefinitionHash: consentEvidenceHash(tool)})
	}
	changes, err := compareCatalogInventory(old, inventory)
	require.NoError(t, err)
	require.Len(t, changes.Added, 1)
	require.Equal(t, "source--added", changes.Added[0].Name)
	require.Len(t, changes.Removed, 1)
	require.Equal(t, "source--removed", changes.Removed[0].Name)
	require.Len(t, changes.Changed, 1)
	require.Equal(t, "source--changed", changes.Changed[0].Name)
	require.Equal(t, []catalogFieldChange{{Name: "description", Before: "old", After: "new", HasBefore: true, HasAfter: true}}, changes.Changed[0].Fields)
}

func TestFreezeConnectionCatalogRejectsInvalidDefinitions(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "freeze_invalid_definitions")
	require.NoError(t, err)
	service := &Service{aiCatalog: aivendorstest.NewStore(), db: pool, logger: testenv.NewLogger(t)}
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: validUUID(uuid.New())}
	subject := urn.NewAnonymousSubject(uuid.NewString())
	state := AuthnChallengeState{ID: uuid.NewString(), Subject: &subject}
	for _, tc := range []struct{ name, tool, hash, want string }{
		{name: "unapproved source", tool: "other--injected", want: "no verified source"},
		{name: "changed definition", tool: "target--changed", hash: "mismatch", want: "catalog is incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			definition := &toolListEntry{Name: tc.tool}
			hash := tc.hash
			if hash == "" {
				hash = consentEvidenceHash(definition)
			}
			inventory := &consentToolInventory{TypedGateway: true, StateID: state.ID, Resource: endpointToolSelectionResource(endpoint), Complete: true, Tools: []consentInventoryTool{{Name: tc.tool, Definition: definition, DefinitionHash: hash}}}
			_, err := service.freezeConnectionCatalog(t.Context(), endpoint, state, state.ClientID, inventory, nil, toolfilter.CatalogModeCached, time.Now().Add(time.Hour))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestReconnectRecordsOnlyReviewedTypedChoices(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := TestInfra.CloneTestDatabase(t, "reconnect_typed_choices")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	issuerID := uuid.New()
	require.NoError(t, selectionbackfill.New(pool).SeedUserSessionIssuerFixture(ctx, selectionbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: uuid.NullUUID{}, OrganizationID: text(f.orgID), Slug: "typed-reconnect"}))
	credentialID := uuid.New()
	unrelated, _ := f.server()
	require.NoError(t, f.q.SeedUpstreamSelectionFixture(ctx, authclientbackfill.SeedUpstreamSelectionFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, CredentialID: credentialID, McpServerID: unrelated, NoAuth: true}))
	client := f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	previous := &connectionCatalog{IssuerID: issuerID, OrganizationID: f.orgID}
	next := &connectionCatalog{IssuerID: issuerID, OrganizationID: f.orgID, Members: map[string]connectionCatalogMember{
		"source": {ServerID: f.serverID, ProjectID: f.projectID, Auth: &connectionCatalogAuth{ClientID: client, RegistrationID: validUUID(f.registration)}},
	}}
	replace := func(previous, next *connectionCatalog) {
		t.Helper()
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return replaceReconnectUpstreamChoices(ctx, usersessionsrepo.New(tx), credentialID, previous, next)
		}))
	}
	params := authclientbackfill.GetSelectionFixtureParams{CredentialID: credentialID, McpServerID: f.serverID}
	replace(previous, next)
	choice, err := f.q.GetSelectionFixture(ctx, params)
	require.NoError(t, err)
	require.Equal(t, validUUID(client), choice.AuthClientID)

	// A retired pick never authorizes a substitute, even when only one remains.
	require.NoError(t, f.q.SoftDeleteAuthClientFixture(ctx, client))
	f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
	replace(next, next)
	_, err = f.q.GetSelectionFixture(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	untouched, err := f.q.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: credentialID, McpServerID: unrelated})
	require.NoError(t, err)
	require.True(t, untouched.NoAuth)

	// A reconnect can keep an EMA binding from a catalog that predates the
	// picker flag. It cannot revive the binding after retirement.
	ema := f.adminClient(authClientTypeEMA, validUUID(f.registration))
	paramsSet := next.upstreamChoices(credentialID)
	paramsSet.PickedAuthClientIds = []uuid.UUID{ema}
	_, err = usersessionsrepo.New(pool).RecordIssuedAuthClientChoices(ctx, paramsSet)
	require.NoError(t, err)
	next.Members["source"].Auth.ClientID = ema
	replace(next, next)
	choice, err = f.q.GetSelectionFixture(ctx, params)
	require.NoError(t, err)
	require.Equal(t, validUUID(ema), choice.AuthClientID)
	require.NoError(t, f.q.SoftDeleteAuthClientFixture(ctx, ema))
	replace(next, next)
	_, err = f.q.GetSelectionFixture(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestFreezeConnectionCatalogRefusesLegacyPreview(t *testing.T) {
	t.Parallel()
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: validUUID(uuid.New())}
	subject := urn.NewAnonymousSubject(uuid.NewString())
	state := AuthnChallengeState{ID: uuid.NewString(), Subject: &subject}
	inventory := &consentToolInventory{StateID: state.ID, Resource: endpointToolSelectionResource(endpoint), Complete: true}
	_, err := (&Service{}).freezeConnectionCatalog(t.Context(), endpoint, state, state.ClientID, inventory, nil, toolfilter.CatalogModeCached, time.Now().Add(time.Hour))
	require.ErrorIs(t, err, errConsentConfigurationChanged)
}
