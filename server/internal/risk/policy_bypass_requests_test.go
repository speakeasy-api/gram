package risk_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRiskPolicyBypassRequest_URLTokenLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass URL Token"),
	})
	require.NoError(t, err)

	fullURL := "https://mcp.example.com/server"
	token := riskPolicyBypassRequestToken(t, authCtx, policy.ID, fullURL)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	require.NotNil(t, request)
	assert.Equal(t, policy.ID, request.PolicyID)
	assert.Equal(t, "requested", request.Status)
	require.NotNil(t, request.TargetKind)
	assert.Equal(t, authz.SelectorKeyServerURL, *request.TargetKind)
	require.NotNil(t, request.TargetKey)
	assert.Equal(t, fullURL, *request.TargetKey)
	assert.Equal(t, fullURL, request.TargetDimensions[authz.SelectorKeyServerURL])
	assert.Equal(t, authCtx.UserID, request.RequesterUserID)

	approved, err := ti.service.ApproveRiskPolicyBypassRequest(ctx, &gen.ApproveRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Status)
	require.Len(t, approved.GrantedPrincipalUrns, 1)
	assert.True(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))

	revoked, err := ti.service.RevokeRiskPolicyBypassRequest(ctx, &gen.RevokeRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "revoked", revoked.Status)
	assert.Empty(t, revoked.GrantedPrincipalUrns)
	assert.False(t, userHasRiskPolicyBypassGrant(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, policy.ID, fullURL))
}

func TestRiskPolicyBypassRequest_ReRequestAfterDenyResetsState(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Policy Bypass Re-request"),
	})
	require.NoError(t, err)

	token := riskPolicyBypassRequestToken(t, authCtx, policy.ID, "https://mcp.example.com/denied")
	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)

	denied, err := ti.service.DenyRiskPolicyBypassRequest(ctx, &gen.DenyRiskPolicyBypassRequestPayload{
		ID: request.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "denied", denied.Status)
	require.NotNil(t, denied.DecidedBy)

	refreshed, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: token,
	})
	require.NoError(t, err)
	assert.Equal(t, request.ID, refreshed.ID)
	assert.Equal(t, "requested", refreshed.Status)
	assert.Nil(t, refreshed.DecidedBy)
	assert.Empty(t, refreshed.GrantedPrincipalUrns)
}

func riskPolicyBypassRequestToken(t *testing.T, authCtx *contextvalues.AuthContext, policyID string, fullURL string) string {
	t.Helper()

	token, _, err := risk.GeneratePolicyBypassRequestToken("test-jwt-secret", risk.PolicyBypassRequestTokenInput{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              authCtx.ProjectID.String(),
		RequesterUserID:        authCtx.UserID,
		ObservedName:           nil,
		ObservedFullURL:        &fullURL,
		ObservedURLHost:        nil,
		ObservedServerIdentity: nil,
		ToolName:               nil,
		ToolCall:               nil,
		BlockReason:            nil,
		RiskPolicyID:           policyID,
		RiskResultID:           nil,
	}, 5*time.Minute)
	require.NoError(t, err)
	return token
}

func userHasRiskPolicyBypassGrant(t *testing.T, ti *testInstance, organizationID, userID, policyID, serverURL string) bool {
	t.Helper()

	grants, err := authz.LoadGrants(t.Context(), ti.conn, organizationID, []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeUser, userID),
	})
	require.NoError(t, err)

	for _, grant := range grants {
		if grant.Scope != authz.ScopeRiskPolicyBypass {
			continue
		}
		if grant.Selector[authz.SelectorKeyResourceID] != policyID {
			continue
		}
		if grant.Selector[authz.SelectorKeyServerURL] != serverURL {
			continue
		}
		return true
	}
	return false
}
