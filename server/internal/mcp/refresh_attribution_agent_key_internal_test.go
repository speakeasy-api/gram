package mcp

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type attributionUpstreamTokens struct {
	fakeUpstreamTokens
	attribution remotesessions.RefreshAttribution
}

func (f *attributionUpstreamTokens) ResolveAuthClientAccessTokens(_ context.Context, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	f.attribution = req.Attribution
	return f.tokens(), nil
}

func (f *attributionUpstreamTokens) ResolveClientAccessTokens(_ context.Context, attribution remotesessions.RefreshAttribution, _ uuid.UUID, _ string, _ uuid.UUID, _ urn.SessionSubject, _ uuid.UUID, _ bool, _ string) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	f.attribution = attribution
	return f.tokens(), nil
}

func TestAgentKeyMemberChoicesPreservesRequestAttribution(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_agent_key_refresh_attribution")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	agentID := uuid.New()
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: uuid.NewString()}, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})
	request := httptest.NewRequest("POST", "https://mcp.example.com/mcp", nil)
	for _, organizationGateway := range []bool{false, true} {
		scope := mcpconnectionattempts.GatewayScope(f.orgID, f.projectID, uuid.New(), mcpconnectionattempts.AppUnverified)
		if organizationGateway {
			scope = mcpconnectionattempts.OrganizationGatewayScope(f.orgID, uuid.New(), mcpconnectionattempts.AppUnverified)
		}
		attribution := remotesessions.RefreshIn(scope).FromRequest(request)
		choices, err := f.service.agentKeyMemberChoices(ctx, f.orgID, agentID, attribution)
		require.NoError(t, err)
		require.NotNil(t, choices)
		for _, typed := range []bool{false, true} {
			source := &attributionUpstreamTokens{fakeUpstreamTokens: fakeUpstreamTokens{issuerID: uuid.New(), bound: uuid.New()}}
			f.service.upstreamTokens = source
			_, err := choices.resolve(ctx, metaMember{serverID: f.serverID, projectID: f.projectID, typedAuthClient: typed, chosenClientID: validUUID(uuid.New())})
			require.NoError(t, err)
			// The client is a func, which require.Equal never treats as equal; %+v compares it by nil-ness and code.
			require.Equal(t, fmt.Sprintf("%+v", attribution), fmt.Sprintf("%+v", source.attribution), "lazy resolution must retain the gateway scope and incoming request")
			require.NotEqual(t, fmt.Sprintf("%+v", remotesessions.RefreshIn(scope)), fmt.Sprintf("%+v", source.attribution), "attribution keeps the incoming request")
		}
	}
}
