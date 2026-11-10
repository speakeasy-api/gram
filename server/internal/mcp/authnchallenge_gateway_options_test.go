package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestConsentGatewayModeChoices(t *testing.T) {
	t.Parallel()
	service := &Service{codeExecutor: codemode.Disabled{}}
	endpoint := &ResolvedMcpEndpoint{MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, OrganizationID: "org_test", ProjectID: uuid.New()}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", OrganizationSlug: "test-org"})
	_, err := service.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "code_mode")
	requireOopsCode(t, err, oops.CodeUnavailable, "a runtime that cannot start runners is retryable")
	entitled := false
	available := &Service{codeExecutor: availableCodeExecutor{}, platformFeatureChecker: func(context.Context, string, string) bool { return entitled }}
	_, err = available.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "code_mode")
	requireOopsCode(t, err, oops.CodeForbidden, "an organization without the entitlement is refused")
	entitled = true
	policy, err := available.consentGatewayPolicy(ctx, endpoint, AuthnChallengeState{}, "", "code_mode")
	require.NoError(t, err)
	require.Equal(t, metamcp.DiscoveryModeCode, *policy.Gateway.DiscoveryMode)
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

// TestConsentGatewayDefaultRecordsMode pins that "Gateway default" records the
// gateway's current default for the connections that can choose a mode, and
// nothing for first-party and agent approvals, which follow the live default.
func TestConsentGatewayDefaultRecordsMode(t *testing.T) {
	t.Parallel()
	service := &Service{codeExecutor: codemode.Disabled{}}
	organizationGateway := &ResolvedMcpEndpoint{MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, OrganizationID: "org_test", RouteBase: mcproute.OrganizationGateway}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", OrganizationSlug: "test-org"})
	policy, err := service.consentGatewayPolicy(ctx, organizationGateway, AuthnChallengeState{}, "", "")
	require.NoError(t, err)
	require.NotNil(t, policy.Gateway.DiscoveryMode)
	require.Equal(t, metamcp.DiscoveryModeProgressive, *policy.Gateway.DiscoveryMode, "the organization gateway defaults to Progressive")
	require.Equal(t, endpointToolSelectionResource(organizationGateway), policy.Resource)
	policy, err = service.consentGatewayPolicy(ctx, organizationGateway, AuthnChallengeState{FirstParty: true}, "", "")
	require.NoError(t, err)
	require.Nil(t, policy)
	policy, err = service.consentGatewayPolicy(ctx, organizationGateway, AuthnChallengeState{}, uuid.NewString(), "")
	require.NoError(t, err)
	require.Nil(t, policy)
}

type availableCodeExecutor struct{ codemode.Disabled }

func (availableCodeExecutor) Available() bool { return true }

func requireOopsCode(t *testing.T, err error, code oops.Code, msg string) {
	t.Helper()
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	require.True(t, ok, msg)
	require.Equal(t, code, shareable.Code, msg)
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
