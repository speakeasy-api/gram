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
		Exception: GrantCheck{Check: writeCheck, Instance: nil},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
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
		Exception: GrantCheck{
			Check:    writeCheck,
			Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_456"},
		},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.True(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonMatched, result.Reason)
}

func TestGrantExpressionEvaluate_nestedDifferencePreservesExceptionReason(t *testing.T) {
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
			Exception: GrantCheck{Check: writeCheck, Instance: instance},
		},
		Exception: GrantCheck{Check: otherCheck, Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_other"}},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
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
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
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
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
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
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
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
	require.Equal(t, GrantExpressionReasonExceptionMatched, result.Reason)
}
