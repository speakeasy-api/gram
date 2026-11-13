package authz

import "slices"

// MCPToolCallDimensions carries the typed attributes of an MCP tool call.
// Zero-value fields are omitted from the check dimensions.
type MCPToolCallDimensions struct {
	Tool        string
	Disposition string
	ProjectID   string
}

// MCPToolCallCheck builds a Check for an MCP tool call with the given dimensions.
// dims.ProjectID is required: evaluation rejects an MCP check without one.
// The check always carries the tool and its disposition. One that is empty
// is carried as unknown, so a grant limited to some tools or dispositions
// never allows it and a block limited to them always applies.
func MCPToolCallCheck(toolsetID string, dims MCPToolCallDimensions) Check {
	dimensions := map[string]string{
		SelectorKeyTool:        valueOrUnknown(dims.Tool),
		SelectorKeyDisposition: valueOrUnknown(dims.Disposition),
	}
	if dims.ProjectID != "" {
		dimensions[SelectorKeyProjectID] = dims.ProjectID
	}
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationToolCall, organization: None[OrgID]()}
}

// validateCheckOperation holds a check to the operation it was built as. A
// tool call carries its tool and disposition, and nothing else carries
// either: a check that names a tool without being built as a tool call is
// refused, so it can never be read as a connection or the reverse.
func validateCheckOperation(c Check) error {
	_, hasTool := c.Dimensions[SelectorKeyTool]
	_, hasDisposition := c.Dimensions[SelectorKeyDisposition]
	if c.isToolCall() {
		if !isConnectScope(c.Scope) {
			return invalidCheckReason(c.Scope, c.ResourceID, "only the connect permission has tool calls")
		}
		if !hasTool || !hasDisposition {
			return invalidCheckReason(c.Scope, c.ResourceID, "a tool call must carry its tool and disposition")
		}
		return nil
	}
	if hasTool || hasDisposition {
		return invalidCheckReason(c.Scope, c.ResourceID, "tool and disposition belong to a tool call")
	}
	return nil
}

// isConnectScope reports whether scope is the MCP connect permission or the
// block that takes it away.
func isConnectScope(scope Scope) bool {
	return scope == ScopeMCPConnect || scope == ScopeMCPBlockedConnect
}

func valueOrUnknown(value string) string {
	if value == "" {
		return SelectorValueUnknown
	}
	return value
}

// MCPCheck builds a Check for an MCP scope (read/write/connect) with project_id
// injected as a dimension so project-scoped grants can match. resourceID is the
// server or toolset ID for operations on one server, or the project ID for
// project-level operations. projectID is required: evaluation rejects an MCP
// check without one.
func MCPCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// AssistantCheck builds a Check for an assistant scope. resourceID is the
// assistant ID for operations on one assistant, or the project ID for
// project-level operations such as creating an assistant. projectID is injected
// as a dimension, so a project-wide grant ({"resource_id":"*","project_id":P})
// covers every assistant in P, while a grant naming one assistant
// ({"resource_id":A}) covers only that assistant.
func AssistantCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// EnvironmentCheck builds a Check for an environment scope. resourceID is the
// environment ID for operations on one environment, or the project ID for
// project-level operations that have no concrete environment (create, or a
// slug that did not resolve). projectID is always injected as a dimension, so a
// project-wide grant ({"resource_id":"*","project_id":P}) covers every
// environment in P, while a grant naming one environment covers only that one.
func EnvironmentCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// HooksIngestCheck builds the org:hooks_ingest check for projectID. It always
// carries the project, because matching skips grant keys a check omits, which
// would let a project-bounded grant through for any project.
func HooksIngestCheck(organizationID, projectID string) Check {
	return Check{Scope: ScopeOrgHooksIngest, ResourceKind: "", ResourceID: organizationID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// MCPAuthClientDimensions carries the trusted attributes of one use of an auth
// client. ProjectID is the project of the server the client serves.
// ConnectionOwner is the stored owner of a personal connection, or the user
// about to create it, and UpstreamClient is the upstream OAuth client the
// connection uses; leave both empty for every client type but remote_session.
type MCPAuthClientDimensions struct {
	ClientType      string
	ProjectID       string
	ConnectionOwner string
	UpstreamClient  string
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
	if dims.UpstreamClient != "" {
		dimensions[SelectorKeyUpstreamClient] = dims.UpstreamClient
	}
	return Check{Scope: ScopeMCPAuth, ResourceKind: "", ResourceID: clientID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// RequiredCheckDimensions returns the dimensions a check of resourceKind must
// carry, or nil when the kind requires none.
func RequiredCheckDimensions(resourceKind string) []string {
	return slices.Clone(requiredCheckDimensions[resourceKind])
}

// validateCheckDimensions rejects dimension values no concrete request can
// carry: an empty string, the wildcard, the represented-user marker, and an
// unknown value on a dimension that must be known.
func validateCheckDimensions(c Check) error {
	for key, value := range c.Dimensions {
		switch value {
		case "":
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" is empty")
		case WildcardResource, SelectorValueRepresentedUser:
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" must be concrete")
		case SelectorValueUnknown:
			if !unknownableDimensions[c.Scope][key] {
				return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" must be known")
			}
		}
	}
	return nil
}

// validateRequiredDimensions rejects a check that omits, or wildcards, a
// dimension its resource kind requires, so it fails closed instead of matching
// a grant bounded on that dimension.
func validateRequiredDimensions(c Check) error {
	for _, key := range requiredCheckDimensions[c.resourceKind()] {
		value := c.Dimensions[key]
		if value == "" {
			return invalidCheckReason(c.Scope, c.ResourceID, "missing "+key)
		}
		if value == WildcardResource {
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" must be concrete")
		}
	}
	return nil
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
	if ResourceKindForScope(c.Scope) != ResourceKindMCPAuthClient {
		return invalidCheckReason(c.Scope, c.ResourceID, "scope must be an auth-client scope")
	}
	clientType := c.Dimensions[SelectorKeyClientType]
	if !validAuthClientTypes[clientType] {
		return invalidCheckReason(c.Scope, c.ResourceID, "unknown client_type")
	}
	if c.Dimensions[SelectorKeyProjectID] == "" {
		return invalidCheckReason(c.Scope, c.ResourceID, "missing project_id")
	}
	personal := clientType == AuthClientTypeRemoteSession
	for _, key := range []string{SelectorKeyConnectionOwner, SelectorKeyUpstreamClient} {
		value, has := c.Dimensions[key]
		if personal && (!has || value == "") {
			return invalidCheckReason(c.Scope, c.ResourceID, "missing "+key+" for a remote_session client")
		}
		if !personal && has {
			return invalidCheckReason(c.Scope, c.ResourceID, key+" is allowed for remote_session clients only")
		}
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
			Check:    check.exclusionCheck(exclusion),
			Instance: instance,
		},
	}
}

type RiskPolicyDimensions struct {
	ServerURL      string
	ServerIdentity string
}

func RiskPolicyEvaluateCheck(policyID string) Check {
	return Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
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
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// ChatWriteCheck builds a Check authorizing destructive mutation of agent chat
// sessions — renaming, attaching feedback, and deleting. It is deliberately
// separate from ChatReadCheck: a session reviewer granted chat:read can read
// every transcript (and pin one as a shared bookmark) but must not be able to
// destroy one. Owner-matching in the chat handlers still lets anyone mutate
// their own sessions without a grant.
func ChatWriteCheck(resourceID string) Check {
	return Check{Scope: ScopeChatWrite, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
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
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, selectorMatch: selectorMatchStrict, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}

// InferenceExecuteCheck authorizes inference in one project, not chat mutation.
// Always include project_id so project-bounded grants cannot match by omission.
func InferenceExecuteCheck(projectID string) Check {
	return Check{Scope: ScopeInferenceExecute, ResourceKind: "", ResourceID: projectID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
}
