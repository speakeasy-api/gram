package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestShadowPolicyDecisionsCountDistinctCanonicalTargets(t *testing.T) {
	t.Parallel()
	policyID := uuid.New()
	disposition := shadowmcp.DispositionBlockAll
	policy := policycore.Policy{ID: policyID, ShadowMCPDisposition: &disposition}
	allowed := []authz.Grant{
		{PrincipalUrn: "user:one", Selector: selectorWithURL(authz.ScopeRiskPolicyBypass, policyID.String(), "https://mcp.example.test/server")},
		{PrincipalUrn: "role:team", Selector: selectorWithURL(authz.ScopeRiskPolicyBypass, policyID.String(), "https://mcp.example.test/server")},
		{PrincipalUrn: "user:two", Selector: authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID.String())},
	}
	blocked := []authz.Grant{{PrincipalUrn: "user:three", Selector: selectorWithURL(authz.ScopeRiskPolicyBlock, policyID.String(), "https://mcp.example.test/blocked")}}

	result, err := shadowPolicyDecisions(policy, allowed, blocked)

	require.NoError(t, err)
	require.Equal(t, &ShadowPolicyDecisions{Disposition: shadowmcp.DispositionBlockAll, AllowedTargetCount: 1, BlockedTargetCount: 1, ManagedVia: "shadow_inventory"}, result)
}

func TestShadowPolicyDecisionsOmitNonShadowPolicies(t *testing.T) {
	t.Parallel()

	result, err := shadowPolicyDecisions(policycore.Policy{}, nil, nil)

	require.NoError(t, err)
	require.Nil(t, result)
}

func TestShadowPolicyDecisionsRejectUnsafeInputsWithoutReturningThem(t *testing.T) {
	t.Parallel()
	disposition := shadowmcp.DispositionAllowAll
	unsafeURL := "://invalid-secret"

	_, err := shadowPolicyDecisionsForURLs(disposition, []string{unsafeURL}, nil)

	require.ErrorIs(t, err, ErrUnavailable)
	require.NotContains(t, err.Error(), unsafeURL)
	require.NotContains(t, err.Error(), "invalid-secret")
}

func TestShadowPolicyDecisionsFromVersionStateMatchesGrantProjection(t *testing.T) {
	t.Parallel()
	policyID := uuid.New()
	disposition := shadowmcp.DispositionAllowAll
	selector := selectorWithURL(authz.ScopeRiskPolicyBlock, policyID.String(), "https://mcp.example.test/blocked")
	raw, err := selector.MarshalJSON()
	require.NoError(t, err)

	result, err := shadowPolicyDecisionsFromVersionState(RiskPolicyVersionState{
		Policy:                policycore.Policy{ID: policyID, ShadowMCPDisposition: &disposition},
		AllowedURLGrants:      []RiskPolicyVersionGrant{},
		BlockedURLGrants:      []RiskPolicyVersionGrant{{PrincipalURN: "user:one", Selector: raw}},
		AnalyzerConfig:        nil,
		StandingDecisionState: []string{},
	})

	require.NoError(t, err)
	require.Equal(t, &ShadowPolicyDecisions{Disposition: shadowmcp.DispositionAllowAll, AllowedTargetCount: 0, BlockedTargetCount: 1, ManagedVia: "shadow_inventory"}, result)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "mcp.example.test")
	require.NotContains(t, string(encoded), "user:one")
}

func selectorWithURL(scope authz.Scope, policyID, url string) authz.Selector {
	selector := authz.NewSelector(scope, policyID)
	selector[authz.SelectorKeyServerURL] = url
	return selector
}
