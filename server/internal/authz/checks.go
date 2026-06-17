package authz

// MCPToolCallDimensions carries the typed attributes of an MCP tool call.
// Zero-value fields are omitted from the check dimensions.
type MCPToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
}

// MCPToolCallCheck builds a Check for an MCP tool call with the given dimensions.
func MCPToolCallCheck(toolsetID string, dims MCPToolCallDimensions) Check {
	dimensions := map[string]string{}
	if dims.Tool != "" {
		dimensions[SelectorKeyTool] = dims.Tool
	}
	if dims.Disposition != "" {
		dimensions[SelectorKeyDisposition] = dims.Disposition
	}
	if dims.ProjectID != "" {
		dimensions[SelectorKeyProjectID] = dims.ProjectID
	}
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, expanded: false}
}

// MCPCheck builds a Check for an MCP scope (read/write/connect) with project_id
// injected as a dimension so project-scoped grants can match.
func MCPCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, expanded: false}
}

func expressionForCheck(check Check) GrantExpression {
	exclusions, ok := scopeExclusions[check.Scope]
	if !ok || len(exclusions) == 0 {
		return nil
	}

	instance := check.selector()
	base := GrantCheck{Check: check, Instance: instance}
	expressions := make([]GrantExpression, 0, len(exclusions))
	for _, exclusion := range exclusions {
		expressions = append(expressions, GrantCheck{
			Check:    Check{Scope: exclusion, ResourceKind: check.ResourceKind, ResourceID: check.ResourceID, Dimensions: check.Dimensions, selectorMatch: selectorMatchStrict, expanded: false},
			Instance: instance,
		})
	}
	return GrantDifference{
		Base:      base,
		Exclusion: GrantUnion{Expressions: expressions},
	}
}

type RiskPolicyDimensions struct {
	ServerURL      string
	ServerIdentity string
}

func RiskPolicyEvaluateCheck(policyID string) Check {
	return Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
}

// RiskPolicyApplies builds the runtime authorization rule for applying a risk
// policy to a request.
//
// The rule is:
//
//	user can evaluate the policy for this request
//	  unless user can bypass the same policy for this request
//
// The evaluate check is intentionally broad because audience grants may only
// name the policy. The expression Instance is built from the bypass check so
// both sides talk about the same concrete request dimensions, such as
// server_url or server_identity.
func RiskPolicyApplies(policyID string, bypassDims RiskPolicyDimensions) GrantExpression {
	bypass := RiskPolicyBypassCheck(policyID, bypassDims)
	instance := bypass.selector()
	return GrantDifference{
		Base:      GrantCheck{Check: RiskPolicyEvaluateCheck(policyID), Instance: instance},
		Exclusion: GrantCheck{Check: bypass, Instance: instance},
	}
}

func RiskPolicyBypassCheck(policyID string, dims RiskPolicyDimensions) Check {
	var dimensions map[string]string
	if dims.ServerURL != "" {
		dimensions = map[string]string{}
		dimensions[SelectorKeyServerURL] = dims.ServerURL
	}
	if dims.ServerIdentity != "" {
		if dimensions == nil {
			dimensions = map[string]string{}
		}
		dimensions[SelectorKeyServerIdentity] = dims.ServerIdentity
	}
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, expanded: false}
}
