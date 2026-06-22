package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantExpressionEvaluate_riskPolicyAppliesWithoutBypass(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_missingBase(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(nil)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMissingBase, result.Reason)
}

func TestEvaluateGrantCheck_rootGrantNotSelfExcluded(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	cases := []struct {
		scope      Scope
		resourceID string
	}{
		{scope: ScopeMCPConnect, resourceID: "tool_a"},
		{scope: ScopeMCPRead, resourceID: "tool_a"},
		{scope: ScopeMCPWrite, resourceID: "tool_a"},
		{scope: ScopeProjectRead, resourceID: "proj_123"},
		{scope: ScopeProjectWrite, resourceID: "proj_123"},
		{scope: ScopeOrgRead, resourceID: "org_456"},
		{scope: ScopeOrgAdmin, resourceID: "org_456"},
		{scope: ScopeEnvironmentRead, resourceID: "env_a"},
		{scope: ScopeEnvironmentWrite, resourceID: "env_a"},
	}

	for _, tc := range cases {
		t.Run(string(tc.scope), func(t *testing.T) {
			t.Parallel()

			eval, err := evaluateGrantCheck(grants, Check{Scope: tc.scope, ResourceKind: "", ResourceID: tc.resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false})
			require.NoError(t, err)
			require.False(t, eval.Denied)
			require.NotNil(t, eval.Grant)
		})
	}
}

func TestGrantExpressionEvaluate_rejectsDenyGrantForBaseScope(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewDenyGrant(ScopeRiskPolicyEvaluate, policyID),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedMixedGrantSemantics)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonError, result.Reason)
}

func TestGrantExpressionEvaluate_rejectsDenyGrantForExceptionScope(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewDenyGrant(ScopeRiskPolicyBypass, policyID),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsupportedMixedGrantSemantics)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonError, result.Reason)
}

func TestGrantExpressionEvaluate_ignoresDenyGrantForUnreferencedScope(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewDenyGrant(ScopeProjectRead, "project_123"),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_rejectsWrappedMixedSemanticsError(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewDenyGrant(ScopeRiskPolicyBypass, policyID),
	}

	_, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.ErrorIs(t, err, ErrUnsupportedMixedGrantSemantics)
}

func TestGrantExpressionEvaluate_differenceWorksForGenericChecks(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}

	result, err := GrantDifference{
		Base:      GrantCheck{Check: readCheck, Instance: nil},
		Exclusion: GrantCheck{Check: writeCheck, Instance: nil},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_allowsDenyGrantForExpandedScope(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectWrite, "proj_123"),
		NewDenyGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}

	result, err := GrantCheck{Check: readCheck, Instance: nil}.Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_differenceKeepsNonMatchingSetKey(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}

	result, err := GrantDifference{
		Base: GrantCheck{
			Check:    readCheck,
			Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_123"},
		},
		Exclusion: GrantCheck{
			Check:    writeCheck,
			Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_456"},
		},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_nestedDifferencePreservesExclusionReason(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	otherCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_other", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	instance := Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_123"}

	result, err := GrantDifference{
		Base: GrantDifference{
			Base:      GrantCheck{Check: readCheck, Instance: instance},
			Exclusion: GrantCheck{Check: writeCheck, Instance: instance},
		},
		Exclusion: GrantCheck{Check: otherCheck, Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_other"}},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_projectWriteBlocklistSubtractsProductionSelector(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	grants := []Grant{
		NewGrant(ScopeProjectWrite, WildcardResource),
		NewGrantWithSelector(ScopeProjectBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindProject,
			SelectorKeyResourceID:   projectID,
		}),
	}

	result, err := expressionForCheck(Check{
		Scope:         ScopeProjectWrite,
		ResourceKind:  "",
		ResourceID:    projectID,
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
		expanded:      false,
	}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)

	result, err = expressionForCheck(Check{
		Scope:         ScopeProjectWrite,
		ResourceKind:  "",
		ResourceID:    "project_other",
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
		expanded:      false,
	}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_projectReadBlocklistSubtractsProjectWrite(t *testing.T) {
	t.Parallel()

	const projectID = "project_123"
	grants := []Grant{
		NewGrant(ScopeProjectWrite, WildcardResource),
		NewGrant(ScopeProjectBlockedRead, projectID),
	}

	result, err := expressionForCheck(Check{
		Scope:         ScopeProjectWrite,
		ResourceKind:  "",
		ResourceID:    projectID,
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
		expanded:      false,
	}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_mcpWriteBlocklistSubtractsProductionSelector(t *testing.T) {
	t.Parallel()

	const projectID = "0196cbd1-9328-74e7-b7bb-6e5357565573"
	grants := []Grant{
		NewGrant(ScopeMCPWrite, WildcardResource),
		NewGrantWithSelector(ScopeMCPBlockedWrite, Selector{
			SelectorKeyResourceKind: ResourceKindMCP,
			SelectorKeyResourceID:   WildcardResource,
			SelectorKeyProjectID:    projectID,
		}),
	}

	result, err := expressionForCheck(MCPCheck(ScopeMCPWrite, "server_in_project", projectID)).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)

	result, err = expressionForCheck(MCPCheck(ScopeMCPWrite, "server_other_project", "project_other")).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)

	result, err = expressionForCheck(Check{
		Scope:         ScopeMCPWrite,
		ResourceKind:  "",
		ResourceID:    "dimensionless_probe",
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
		expanded:      false,
	}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_mcpReadBlocklistSubtractsMCPWrite(t *testing.T) {
	t.Parallel()

	const projectID = "project_123"
	grants := []Grant{
		NewGrant(ScopeMCPWrite, WildcardResource),
		NewGrantWithSelector(ScopeMCPBlockedRead, Selector{
			SelectorKeyResourceKind: ResourceKindMCP,
			SelectorKeyResourceID:   WildcardResource,
			SelectorKeyProjectID:    projectID,
		}),
	}

	result, err := expressionForCheck(MCPCheck(ScopeMCPWrite, "server_in_project", projectID)).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)

	result, err = expressionForCheck(MCPCheck(ScopeMCPWrite, "server_other_project", "project_other")).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_mcpConnectBlocklistSubtractsMCPWrite(t *testing.T) {
	t.Parallel()

	const serverID = "server_123"
	grants := []Grant{
		NewGrant(ScopeMCPWrite, WildcardResource),
		NewGrant(ScopeMCPBlockedConnect, serverID),
	}

	result, err := expressionForCheck(Check{
		Scope:         ScopeMCPWrite,
		ResourceKind:  "",
		ResourceID:    serverID,
		Dimensions:    nil,
		selectorMatch: selectorMatchNormal,
		expanded:      false,
	}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_riskPolicyBypassIsEvaluateExclusion(t *testing.T) {
	t.Parallel()

	const policyID = "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewGrant(ScopeRiskPolicyBypass, policyID),
	}

	result, err := expressionForCheck(RiskPolicyEvaluateCheck(policyID)).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_riskPolicyAppliesSubtractsWholePolicyBypass(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewGrant(ScopeRiskPolicyBypass, policyID),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_riskPolicyAppliesSubtractsWildcardBypass(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
			SelectorKeyResourceKind: ResourceKindRiskPolicy,
			SelectorKeyResourceID:   WildcardResource,
		}),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_riskPolicyAppliesIgnoresNonMatchingScopedBypass(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
			SelectorKeyResourceKind: ResourceKindRiskPolicy,
			SelectorKeyResourceID:   policyID,
			SelectorKeyServerURL:    "https://api.example.com",
		}),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)

	result, err = RiskPolicyApplies(policyID, RiskPolicyDimensions{ServerURL: "https://api.example.com"}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_riskPolicyAppliesSubtractsMatchingServerIdentityBypass(t *testing.T) {
	t.Parallel()

	policyID := "policy_123"
	grants := []Grant{
		NewGrant(ScopeRiskPolicyEvaluate, policyID),
		NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
			SelectorKeyResourceKind:   ResourceKindRiskPolicy,
			SelectorKeyResourceID:     policyID,
			SelectorKeyServerIdentity: "github",
		}),
	}

	result, err := RiskPolicyApplies(policyID, RiskPolicyDimensions{ServerIdentity: "gitlab"}).Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)

	result, err = RiskPolicyApplies(policyID, RiskPolicyDimensions{ServerIdentity: "github"}).Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}
