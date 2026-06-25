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

// ChatReadCheck builds a Check authorizing read access to a single chat
// session. ownerUserID carries the chat owner's user id as the user_id
// dimension so a self-scoped grant (chat:read with user_id=<self>) matches only
// the caller's own sessions, while an unconstrained chat:read grant (no user_id
// key — held by admins) matches any owner.
func ChatReadCheck(chatID, ownerUserID string) Check {
	// Always carry the user_id dimension — even when empty — so a self-scoped
	// grant's user_id constraint is enforced. Omitting it would let a member's
	// {user_id:<self>} grant match a session with no owner (every external /
	// Elements chat), because selector matching skips dimensions the check does
	// not constrain. An empty owner only matches an unconstrained (admin) grant.
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: chatID, Dimensions: map[string]string{SelectorKeyUserID: ownerUserID}, selectorMatch: selectorMatchNormal, expanded: false}
}

// ChatReadAllCheck builds a Check that is satisfied only by an UNCONSTRAINED
// chat:read grant (one with no user_id dimension, or user_id="*" — held by
// admins). A member's self-scoped grant carries user_id=<self>, which does not
// match the wildcard user_id this check carries, so the check fails for members.
// Use it to decide list visibility: pass when the caller may see every owner's
// sessions, fail when they are constrained to their own.
//
// resourceID is a placeholder that does not affect matching: every chat:read
// grant wildcards resource_id, so any concrete (non-empty, non-"*") value works.
func ChatReadAllCheck(resourceID string) Check {
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyUserID: WildcardResource}, selectorMatch: selectorMatchNormal, expanded: false}
}

// ChatReadSelfGrant builds the synthetic grant that authorizes a user to read
// their own chat sessions. It is injected into the request grant set so members
// (whose role does not carry a wildcard chat:read) can still load sessions they
// own, while admins additionally hold an unconstrained chat:read grant that
// covers every owner.
func ChatReadSelfGrant(userID string) Grant {
	return NewGrantWithSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyUserID:       userID,
	})
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
