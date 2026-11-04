package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBuiltinConsentCredentialMembers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		member metaMember
		want   bool
	}{
		{name: "slack", member: metaMember{backend: metaMemberBackendBuiltin, builtinKey: builtinmcp.Slack}, want: true},
		{name: "approvals unchanged", member: metaMember{backend: metaMemberBackendBuiltin, builtinKey: "approvals"}},
		{name: "unknown builtin", member: metaMember{backend: metaMemberBackendBuiltin, builtinKey: "unknown"}},
		{name: "proxied", member: metaMember{backend: metaMemberBackendProxied}, want: true},
		{name: "hosted", member: metaMember{backend: metaMemberBackendHosted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, consentMemberHasAuthChoices(tc.member))
		})
	}
}

func TestBuiltinConsentGatewayChoicesAndInventoryBinding(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "builtin_consent_choices")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	require.NoError(t, builtinmcp.EnsureServer(t.Context(), pool, f.orgID, f.projectID, builtinmcp.Slack, uuid.NullUUID{}))
	f.serverID = builtinmcp.ID(f.projectID, builtinmcp.Slack)
	f.enforce()
	first := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	second := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	for _, id := range []uuid.UUID{first, second} {
		f.copyHeader(id, "Authorization", true, false, text("Bearer fixture-token"), pgtype.Text{})
	}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
	member := metaMember{backend: metaMemberBackendBuiltin, builtinKey: builtinmcp.Slack, serverID: f.serverID, projectID: f.projectID, slug: "builtin_slack"}
	req := httptest.NewRequest(http.MethodGet, "/?"+gatewayAuthClientParam+member.slug+"="+second.String(), nil)
	choices, err := f.service.gatewayConsentAuthChoices(t.Context(), req, endpoint, urn.NewUserSubject("user_1"), []metaMember{member}, nil)
	require.NoError(t, err)
	require.NotNil(t, choices[f.serverID])
	require.Equal(t, second, choices[f.serverID].Selected().ID)

	draft := consentToolInventory{Complete: true, DirectAuthClient: &first}
	require.NoError(t, f.service.validateDirectConsentInventory(t.Context(), endpoint, &draft, map[uuid.UUID]uuid.UUID{f.serverID: first}))
	require.ErrorIs(t, f.service.validateDirectConsentInventory(t.Context(), endpoint, &draft, map[uuid.UUID]uuid.UUID{f.serverID: second}), errConsentConfigurationChanged)
	require.ErrorIs(t, f.service.validateDirectConsentInventory(t.Context(), endpoint, &draft, nil), errConsentConfigurationChanged)

	f.service.authClientPermitter = denyPermitter{}
	choices, err = f.service.gatewayConsentAuthChoices(t.Context(), req, endpoint, urn.NewUserSubject("user_1"), []metaMember{member}, nil)
	require.NoError(t, err)
	require.Equal(t, consentAuthNoAccess, choices[f.serverID].Status)
}

func TestBuiltinConsentTransportRoutes(t *testing.T) {
	t.Parallel()
	found := map[string]bool{}
	for _, route := range (&Service{}).builtinOAuthRoutes() {
		if route.path == BuiltinRoute+"/connect/mcp" {
			found[route.method] = true
			require.True(t, route.onAuthenticationHost)
			require.NotNil(t, route.serve)
		}
	}
	require.Equal(t, map[string]bool{http.MethodPost: true, http.MethodDelete: true}, found)
}
