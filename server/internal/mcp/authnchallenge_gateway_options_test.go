package mcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/stretchr/testify/require"
)

func TestConsentGatewayModeChoices(t *testing.T) {
	t.Parallel()
	service := &Service{
		aiCatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
	}
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, OrganizationID: "org_test", ProjectID: uuid.New()}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", OrganizationSlug: "test-org"})
	policy, err := service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "")
	require.NoError(t, err)
	require.Nil(t, policy)
	policy, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "direct")
	require.NoError(t, err)
	require.Nil(t, policy.Selection)
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = toolfilter.ParseSessionSelection(raw)
	require.Error(t, err, "old servers must reject gateway policies")
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{FirstParty: true}, "", "direct")
	require.Error(t, err)
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, uuid.NewString(), "direct")
	require.Error(t, err)
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "unsupported")
	require.Error(t, err)
	organizationGateway := &ResolvedMcpEndpoint{MetaMcpServerID: endpoint.MetaMcpServerID, OrganizationID: "org_test", RouteBase: mcproute.OrganizationGateway}
	policy, err = service.consentGatewayPolicy(ctx, organizationGateway, AuthnChallengeState{}, "", "direct")
	require.NoError(t, err, "organization gateway connections choose their own mode")
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
	require.Equal(t, endpointToolSelectionResource(organizationGateway), policy.Resource)
	_, err = service.consentGatewayPolicy(ctx, organizationGateway, AuthnChallengeState{FirstParty: true}, "", "direct")
	require.Error(t, err)
}

func TestGatewayGrantNamespaceSeparatesOldReaders(t *testing.T) {
	t.Parallel()
	code := gatewayAuthorizationCodePrefix + "opaque-code"
	require.Equal(t, "gatewayUserSessionGrant:"+uuid.Nil.String()+":"+code, userSessionGrantCacheKey(uuid.Nil, code, false))
	require.NotEqual(t, "userSessionGrant:"+uuid.Nil.String()+":"+code, userSessionGrantCacheKey(uuid.Nil, code, false))
}

func TestCatalogGrantNamespaceSeparatesGatewayV1Readers(t *testing.T) {
	t.Parallel()
	code := catalogAuthorizationCodePrefix + "opaque-code"
	key := userSessionGrantCacheKey(uuid.Nil, code, false)
	require.Equal(t, "gatewayCatalogGrant:"+uuid.Nil.String()+":"+code, key)
	require.NotEqual(t, "userSessionGrant:"+uuid.Nil.String()+":"+code, key)
	require.NotEqual(t, "gatewayUserSessionGrant:"+uuid.Nil.String()+":"+code, key)
}
