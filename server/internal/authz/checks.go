package authz

// MCPToolCallDimensions carries the typed attributes of an MCP tool call.
// Zero-value fields are omitted from the check dimensions.
type MCPToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
	// ToolAnnotations is one of ToolAnnotationsKnown, ToolAnnotationsNone, or
	// ToolAnnotationsUnknown — what is known about the tool's disposition.
	// Read paths that populate it must always set one of the three: deny
	// grants on {tool_annotations:"unknown"} or {tool_annotations:"none"}
	// strict-match only when the key is emitted, so neither state can ride
	// the zero-value-omitted convention.
	ToolAnnotations string
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
	if dims.ToolAnnotations != "" {
		dimensions[SelectorKeyToolAnnotations] = dims.ToolAnnotations
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
	exclusion, ok := ExclusionScopeFor(check.Scope)
	if !ok {
		return nil
	}

	instance := check.selector()
	base := GrantCheck{Check: check, Instance: instance}
	return GrantDifference{
		Base: base,
		Exclusion: GrantCheck{
			Check:    Check{Scope: exclusion, ResourceKind: check.ResourceKind, ResourceID: check.ResourceID, Dimensions: check.Dimensions, selectorMatch: selectorMatchStrict, expanded: false},
			Instance: instance,
		},
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

// ChatReadCheck builds a Check authorizing read access to agent chat sessions.
// It is satisfied only by an unrestricted chat:read grant (held by admins).
// Members hold no chat:read grant; their access to sessions they own is granted
// by owner-matching in the chat handlers, not by this scope. resourceID is a
// placeholder that does not affect matching — every chat:read grant wildcards
// resource_id — so any concrete value (the chat or project id) works.
func ChatReadCheck(resourceID string) Check {
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, expanded: false}
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
