package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
