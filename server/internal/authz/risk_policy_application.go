package authz

import "fmt"

// RiskPolicyApplication says whether a risk policy governs a request. The zero
// value applies, so a result nobody filled in never lets a request past a
// policy.
type RiskPolicyApplication string

const (
	// RiskPolicyGoverns means the policy must be enforced: the caller is in
	// its audience with no exemption for this request, or neither could be
	// determined.
	RiskPolicyGoverns RiskPolicyApplication = ""
	// RiskPolicyExempt means the caller is in the audience and an exemption
	// covers this request.
	RiskPolicyExempt RiskPolicyApplication = "exempt"
	// RiskPolicyNotInAudience means no grant places the caller in the policy's
	// audience.
	RiskPolicyNotInAudience RiskPolicyApplication = "not_in_audience"
)

// Applies reports whether the policy governs the request. Only the two named
// reasons for leaving a caller alone say no.
func (a RiskPolicyApplication) Applies() bool {
	return a != RiskPolicyExempt && a != RiskPolicyNotInAudience
}

// EvaluateRiskPolicy decides whether the policy governs a request made with
// these grants. When the question cannot be answered it returns
// RiskPolicyGoverns together with the error: a policy that cannot be evaluated
// applies, and the caller logs the error rather than dropping the policy.
func EvaluateRiskPolicy(grants []Grant, policyID string, dims RiskPolicyDimensions) (RiskPolicyApplication, error) {
	result, err := RiskPolicyApplies(policyID, dims).Evaluate(grants)
	if err != nil {
		return RiskPolicyGoverns, fmt.Errorf("evaluate risk policy application: %w", err)
	}
	switch {
	case result.Satisfied:
		return RiskPolicyGoverns, nil
	case result.Reason == GrantExpressionReasonMissingBase:
		return RiskPolicyNotInAudience, nil
	case result.Reason == GrantExpressionReasonExclusionMatched:
		return RiskPolicyExempt, nil
	default:
		return RiskPolicyGoverns, fmt.Errorf("evaluate risk policy application: unexpected reason %q", result.Reason)
	}
}
