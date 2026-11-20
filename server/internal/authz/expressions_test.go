package authz

import (
	"slices"
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

			var dimensions map[string]string
			if slices.Contains(requiredCheckDimensions[ResourceKindForScope(tc.scope)], SelectorKeyProjectID) {
				dimensions = map[string]string{SelectorKeyProjectID: testProjectID}
			}
			eval, err := evaluateGrantCheck(grants, Check{Scope: tc.scope, ResourceKind: "", ResourceID: tc.resourceID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault})
			require.NoError(t, err)
			require.False(t, eval.Denied)
			require.NotNil(t, eval.Grant)
		})
	}
}

func TestGrantExpressionEvaluate_differenceWorksForGenericChecks(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}

	result, err := GrantDifference{
		Base:      GrantCheck{Check: readCheck, Instance: nil},
		Exclusion: GrantCheck{Check: writeCheck, Instance: nil},
	}.Evaluate(grants)
	require.NoError(t, err)
	require.False(t, result.Satisfied)
	require.Equal(t, GrantExpressionReasonExclusionMatched, result.Reason)
}

func TestGrantExpressionEvaluate_differenceKeepsNonMatchingSetKey(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		NewGrant(ScopeProjectRead, "proj_123"),
		NewGrant(ScopeProjectWrite, "proj_123"),
	}
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}

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
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
	otherCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_other", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
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

func TestEvaluateRiskPolicy_NamesWhyAPolicyDoesNotApply(t *testing.T) {
	t.Parallel()

	const policy = "policy-one"
	audience := NewGrant(ScopeRiskPolicyEvaluate, policy)
	exemption := NewGrant(ScopeRiskPolicyBypass, policy)

	for name, tc := range map[string]struct {
		grants []Grant
		want   RiskPolicyApplication
	}{
		"in the audience":                {grants: []Grant{audience}, want: RiskPolicyGoverns},
		"in the audience and exempt":     {grants: []Grant{audience, exemption}, want: RiskPolicyExempt},
		"outside the audience":           {grants: nil, want: RiskPolicyNotInAudience},
		"audience of another policy":     {grants: []Grant{NewGrant(ScopeRiskPolicyEvaluate, "policy-two")}, want: RiskPolicyNotInAudience},
		"exemption for another policy":   {grants: []Grant{audience, NewGrant(ScopeRiskPolicyBypass, "policy-two")}, want: RiskPolicyGoverns},
		"exemption without the audience": {grants: []Grant{exemption}, want: RiskPolicyNotInAudience},
	} {
		application, err := EvaluateRiskPolicy(tc.grants, policy, RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
		require.NoError(t, err, name)
		require.Equal(t, tc.want, application, name)
		require.Equal(t, tc.want == RiskPolicyGoverns, application.Applies(), name)
	}
}

// A policy that cannot be evaluated applies: the error is for the caller to
// log, never a reason to let the request past the policy.
func TestEvaluateRiskPolicy_APolicyThatCannotBeEvaluatedApplies(t *testing.T) {
	t.Parallel()

	everything := []Grant{NewGrant(ScopeRiskPolicyEvaluate, WildcardResource), NewGrant(ScopeRiskPolicyBypass, WildcardResource)}
	for _, policyID := range []string{"", WildcardResource} {
		application, err := EvaluateRiskPolicy(everything, policyID, RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""})
		require.ErrorIs(t, err, ErrInvalidCheck, "%q", policyID)
		require.Equal(t, RiskPolicyGoverns, application, "%q", policyID)
		require.True(t, application.Applies(), "%q", policyID)
	}

	var unset RiskPolicyApplication
	require.True(t, unset.Applies(), "a result nobody filled in applies")
}
