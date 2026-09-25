package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/stretchr/testify/require"
)

func TestConsentGatewayModeChoices(t *testing.T) {
	t.Parallel()
	enabled := false
	service := &Service{platformFeatureChecker: func(_ context.Context, orgID, name string) bool {
		require.Equal(t, "org_test", orgID)
		require.Equal(t, string(productfeatures.FeatureGatewayDiscoveryModes), name)
		return enabled
	}}
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, OrganizationID: "org_test", ProjectID: uuid.New()}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", OrganizationSlug: "test-org"})
	policy, err := service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "", nil)
	require.NoError(t, err)
	require.Nil(t, policy)
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "direct", nil)
	require.Error(t, err)
	enabled = true
	policy, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "direct", nil)
	require.NoError(t, err)
	require.Nil(t, policy.Selection)
	require.Equal(t, metamcp.DiscoveryModeDirect, *policy.Gateway.DiscoveryMode)
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = toolfilter.ParseSessionSelection(raw)
	require.Error(t, err, "old servers must reject gateway policies")
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{FirstParty: true}, "", "direct", nil)
	require.Error(t, err)
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, uuid.NewString(), "direct", nil)
	require.Error(t, err)
	_, err = service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "unsupported", nil)
	require.Error(t, err)
}

func TestGatewayGrantNamespaceSeparatesOldReaders(t *testing.T) {
	t.Parallel()
	code := gatewayAuthorizationCodePrefix + "opaque-code"
	require.Equal(t, "gatewayUserSessionGrant:"+uuid.Nil.String()+":"+code, userSessionGrantCacheKey(uuid.Nil, code, false))
	require.NotEqual(t, "userSessionGrant:"+uuid.Nil.String()+":"+code, userSessionGrantCacheKey(uuid.Nil, code, false))
}
