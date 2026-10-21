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
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
}

// MCPCheck builds a Check for an MCP scope (read/write/connect) with project_id
// injected as a dimension so project-scoped grants can match.
func MCPCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
}

// AssistantCheck builds a Check for an assistant scope. resourceID is the
// assistant ID for operations on one assistant, or the project ID for
// project-level operations such as creating an assistant. projectID is injected
// as a dimension, so a project-wide grant ({"resource_id":"*","project_id":P})
// covers every assistant in P, while a grant naming one assistant
// ({"resource_id":A}) covers only that assistant.
func AssistantCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: ""}
}

// HooksIngestCheck builds the org:hooks_ingest check for projectID. It always
// carries the project, because matching skips grant keys a check omits, which
// would let a project-bounded grant through for any project.
func HooksIngestCheck(organizationID, projectID string) Check {
	return Check{Scope: ScopeOrgHooksIngest, ResourceKind: "", ResourceID: organizationID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: ""}
}

// MCPAuthClientDimensions carries the trusted attributes of one use of an auth
// client. ProjectID is the project of the server the client serves.
// ConnectionOwner is the stored owner of a personal connection, or the user
// about to create it; leave it empty for every client type but remote_session.
type MCPAuthClientDimensions struct {
	ClientType      string
	ProjectID       string
	ConnectionOwner string
}

// MCPAuthClientCheck builds the mcp:auth check for using clientID. It always
// matches strictly, and evaluation rejects it when a required dimension is
// missing. A personal check is denied unless the connection owner is the user
// the request represents. Where the engine does not enforce RBAC, such as for
// legacy API keys, every auth-client check is denied.
func MCPAuthClientCheck(clientID string, dims MCPAuthClientDimensions) Check {
	dimensions := map[string]string{
		SelectorKeyClientType: dims.ClientType,
		SelectorKeyProjectID:  dims.ProjectID,
	}
	if dims.ConnectionOwner != "" {
		dimensions[SelectorKeyConnectionOwner] = dims.ConnectionOwner
	}
	return Check{Scope: ScopeMCPAuth, ResourceKind: "", ResourceID: clientID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: ""}
}

// validateAuthClientCheck rejects auth-client checks missing the trusted
// dimensions strict matching depends on, so a malformed check cannot fall
// through to a grant that constrains less than intended.
func validateAuthClientCheck(c Check) error {
	if !c.isAuthClientCheck() {
		return nil
	}
	if c.ResourceKind != "" && c.ResourceKind != ResourceKindMCPAuthClient {
		return invalidCheckReason(c.Scope, c.ResourceID, "resource kind must be "+ResourceKindMCPAuthClient)
	}
	clientType := c.Dimensions[SelectorKeyClientType]
	if !validAuthClientTypes[clientType] {
		return invalidCheckReason(c.Scope, c.ResourceID, "unknown client_type")
	}
	if c.Dimensions[SelectorKeyProjectID] == "" {
		return invalidCheckReason(c.Scope, c.ResourceID, "missing project_id")
	}
	owner, hasOwner := c.Dimensions[SelectorKeyConnectionOwner]
	if hasOwner != (clientType == AuthClientTypeRemoteSession) || (hasOwner && owner == "") {
		return invalidCheckReason(c.Scope, c.ResourceID, "connection_owner is required for remote_session clients only")
	}
	for key, value := range c.Dimensions {
		if !allowedSelectorKeys[ResourceKindMCPAuthClient][key] {
			return invalidCheckReason(c.Scope, c.ResourceID, "unexpected dimension "+key)
		}
		if value == WildcardResource || value == SelectorValueRepresentedUser {
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" must be concrete")
		}
	}
	return nil
}

// personalCheckUnbound reports whether c uses a personal connection that is
// not the represented user's own. Ownership filters permission and never
// grants it, so no grant can authorize another user's connection.
func personalCheckUnbound(c Check) bool {
	if !c.isAuthClientCheck() {
		return false
	}
	owner, personal := c.Dimensions[SelectorKeyConnectionOwner]
	return personal && (c.representedUser == "" || owner != c.representedUser)
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
			Check:    Check{Scope: exclusion, ResourceKind: check.ResourceKind, ResourceID: check.ResourceID, Dimensions: check.Dimensions, selectorMatch: selectorMatchStrict, representedUser: check.representedUser},
			Instance: instance,
		},
	}
}

type RiskPolicyDimensions struct {
	ServerURL      string
	ServerIdentity string
}

func RiskPolicyEvaluateCheck(policyID string) Check {
	return Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
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
// It is satisfied by an unrestricted chat:read grant, or by chat:write via
// scope expansion. No system role holds either — not even admin — so access to
// sessions a caller owns comes from owner-matching in the chat handlers, not
// from this scope. resourceID is a placeholder that does not affect matching —
// every chat grant wildcards resource_id — so any concrete value (the chat or
// project id) works.
func ChatReadCheck(resourceID string) Check {
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
}

// ChatWriteCheck builds a Check authorizing destructive mutation of agent chat
// sessions — renaming, attaching feedback, and deleting. It is deliberately
// separate from ChatReadCheck: a session reviewer granted chat:read can read
// every transcript (and pin one as a shared bookmark) but must not be able to
// destroy one. Owner-matching in the chat handlers still lets anyone mutate
// their own sessions without a grant.
func ChatWriteCheck(resourceID string) Check {
	return Check{Scope: ScopeChatWrite, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
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
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: ""}
}
