package authz

import "testing"

var benchmarkExpressionResult GrantExpressionResult

func BenchmarkGrantExpressionEvaluate(b *testing.B) {
	const policyID = "policy_123"
	scopedBypass := NewGrantWithSelector(ScopeRiskPolicyBypass, Selector{
		SelectorKeyResourceKind: ResourceKindRiskPolicy,
		SelectorKeyResourceID:   policyID,
		SelectorKeyServerURL:    "https://api.example.com",
	})

	cases := []struct {
		name       string
		expression GrantExpression
		grants     []Grant
	}{
		{
			name:       "risk_policy_no_bypass",
			expression: RiskPolicyApplies(policyID, RiskPolicyDimensions{}),
			grants:     []Grant{NewGrant(ScopeRiskPolicyEvaluate, policyID)},
		},
		{
			name:       "risk_policy_whole_policy_bypass",
			expression: RiskPolicyApplies(policyID, RiskPolicyDimensions{}),
			grants: []Grant{
				NewGrant(ScopeRiskPolicyEvaluate, policyID),
				NewGrant(ScopeRiskPolicyBypass, policyID),
			},
		},
		{
			name:       "risk_policy_scoped_bypass_match",
			expression: RiskPolicyApplies(policyID, RiskPolicyDimensions{ServerURL: "https://api.example.com"}),
			grants: []Grant{
				NewGrant(ScopeRiskPolicyEvaluate, policyID),
				scopedBypass,
			},
		},
		{
			name:       "risk_policy_scoped_bypass_no_match",
			expression: RiskPolicyApplies(policyID, RiskPolicyDimensions{ServerURL: "https://other.example.com"}),
			grants: []Grant{
				NewGrant(ScopeRiskPolicyEvaluate, policyID),
				scopedBypass,
			},
		},
		{
			name: "project_difference_non_matching_instance",
			expression: GrantDifference{
				Base: GrantCheck{
					Check:    Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false},
					Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_123"},
				},
				Exclusion: GrantCheck{
					Check:    Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false},
					Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_456"},
				},
			},
			grants: []Grant{
				NewGrant(ScopeProjectRead, "proj_123"),
				NewGrant(ScopeProjectWrite, "proj_123"),
			},
		},
		{
			name:       "nested_difference",
			expression: nestedBenchmarkExpression(),
			grants: []Grant{
				NewGrant(ScopeProjectRead, "proj_123"),
				NewGrant(ScopeProjectWrite, "proj_123"),
			},
		},
		{
			name:       "project_write_expression_no_deny",
			expression: expressionForCheck(Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}),
			grants: []Grant{
				NewGrant(ScopeProjectWrite, WildcardResource),
			},
		},
		{
			name:       "project_write_expression_read_deny_match",
			expression: expressionForCheck(Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}),
			grants: []Grant{
				NewGrant(ScopeProjectWrite, WildcardResource),
				NewGrant(ScopeProjectBlockedRead, "proj_123"),
			},
		},
		{
			name:       "mcp_write_expression_no_deny",
			expression: expressionForCheck(MCPCheck(ScopeMCPWrite, "server_123", "proj_123")),
			grants: []Grant{
				NewGrant(ScopeMCPWrite, WildcardResource),
			},
		},
		{
			name:       "mcp_write_expression_read_deny_match",
			expression: expressionForCheck(MCPCheck(ScopeMCPWrite, "server_123", "proj_123")),
			grants: []Grant{
				NewGrant(ScopeMCPWrite, WildcardResource),
				NewGrantWithSelector(ScopeMCPBlockedRead, Selector{
					SelectorKeyResourceKind: ResourceKindMCP,
					SelectorKeyResourceID:   WildcardResource,
					SelectorKeyProjectID:    "proj_123",
				}),
			},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				result, err := tc.expression.Evaluate(tc.grants)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkExpressionResult = result
			}
		})
	}
}

func nestedBenchmarkExpression() GrantExpression {
	readCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	writeCheck := Check{Scope: ScopeProjectWrite, ResourceKind: "", ResourceID: "proj_123", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	otherCheck := Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "proj_other", Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
	instance := Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_123"}

	return GrantDifference{
		Base: GrantDifference{
			Base:      GrantCheck{Check: readCheck, Instance: instance},
			Exclusion: GrantCheck{Check: writeCheck, Instance: instance},
		},
		Exclusion: GrantCheck{
			Check:    otherCheck,
			Instance: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: "proj_other"},
		},
	}
}
