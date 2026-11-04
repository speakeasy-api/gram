package authz

import "fmt"

// RiskPolicyRef names one risk policy, as it applies to one request, for
// typed operations. The request dimensions identify the MCP server a request
// reaches, so a bypass bounded to one server subtracts only from requests to
// that server.
type RiskPolicyRef struct {
	id   string
	dims RiskPolicyDimensions
}

// RiskPolicy names a risk policy for a request with the given dimensions.
// Leave both dimensions empty for a decision about the policy as a whole.
func RiskPolicy(policyID string, dims RiskPolicyDimensions) RiskPolicyRef {
	return RiskPolicyRef{id: policyID, dims: dims}
}

func (r RiskPolicyRef) dimensions() map[string]string {
	var dimensions map[string]string
	if r.dims.ServerURL != "" {
		dimensions = map[string]string{SelectorKeyServerURL: r.dims.ServerURL}
	}
	if r.dims.ServerIdentity != "" {
		if dimensions == nil {
			dimensions = map[string]string{}
		}
		dimensions[SelectorKeyServerIdentity] = r.dims.ServerIdentity
	}
	return dimensions
}

// Evaluate asks whether the policy applies to the request: its audience
// includes the caller and no bypass for the same request subtracts it.
func (r RiskPolicyRef) Evaluate() Operation {
	return newOperation("risk_policy", "evaluate", r.id, r.dimensions())
}

// Bypass asks whether the caller may skip the policy for the request. A
// bypass grant must name every dimension it is bounded by, so it matches
// strictly.
func (r RiskPolicyRef) Bypass() Operation {
	return newOperation("risk_policy", "bypass", r.id, r.dimensions()).WithStrictMatch()
}

// legacyGrantDecision answers loaded-grant questions whose engine-era answer
// is not one check: risk policy applicability is raw subtraction of a bypass
// from the audience, which the engine expressed as RiskPolicyApplies rather
// than a single scope check. ok is false for every other permission.
func legacyGrantDecision(grants []Grant, op Operation) (allowed bool, ok bool, err error) {
	if op.permission.Type != "risk_policy" {
		return false, false, nil
	}
	dims := RiskPolicyDimensions{
		ServerURL:      op.instance.dimensions[SelectorKeyServerURL],
		ServerIdentity: op.instance.dimensions[SelectorKeyServerIdentity],
	}
	switch op.permission.Permission {
	case "evaluate":
		result, err := RiskPolicyApplies(op.instance.resourceID, dims).Evaluate(grants)
		if err != nil {
			return false, true, fmt.Errorf("evaluate risk policy application: %w", err)
		}
		return result.Satisfied, true, nil
	case "bypass":
		return GrantsSatisfy(grants, RiskPolicyBypassCheck(op.instance.resourceID, dims)), true, nil
	default:
		return false, false, nil
	}
}
