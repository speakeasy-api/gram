package authz

import (
	"context"
	"slices"
)

// validateCheckOperation holds a check to the operation it was built as.
// Only a tool call carries a tool or a disposition: a check that names a tool
// without being built as a tool call is refused, so it can never be read as a
// connection or the reverse. A tool call may still leave either out: a tool
// whose annotations are unknown has no disposition to carry, and a grant
// limited to a disposition refuses it.
func validateCheckOperation(c Check) error {
	_, hasTool := c.Dimensions[SelectorKeyTool]
	_, hasDisposition := c.Dimensions[SelectorKeyDisposition]
	if c.isToolCall() {
		if !isConnectScope(c.Scope) {
			return invalidCheckReason(c.Scope, c.ResourceID, "only the connect permission has tool calls")
		}
		return nil
	}
	if hasTool || hasDisposition {
		return invalidCheckReason(c.Scope, c.ResourceID, "tool and disposition belong to a tool call")
	}
	if c.operation == operationConnection && !isConnectScope(c.Scope) {
		return invalidCheckReason(c.Scope, c.ResourceID, "only the connect permission has connections")
	}
	if c.operation == operationPolicyAudience && c.Scope != ScopeRiskPolicyEvaluate && c.Scope != ScopeRiskPolicyBypass {
		return invalidCheckReason(c.Scope, c.ResourceID, "only the risk policy audience permission, or the bypass that takes it away, asks an audience question")
	}
	return nil
}

// isConnectScope reports whether scope is the MCP connect permission or the
// block that takes it away.
func isConnectScope(scope Scope) bool {
	return scope == ScopeMCPConnect || scope == ScopeMCPBlockedConnect
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
	return Check{Scope: ScopeMCPAuth, ResourceKind: "", ResourceID: clientID, Dimensions: dimensions, exact: true, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}

// RequiredCheckDimensions returns the dimensions a check of resourceKind must
// carry, or nil when the kind requires none.
func RequiredCheckDimensions(resourceKind string) []string {
	return slices.Clone(requiredCheckDimensions[resourceKind])
}

// validateCheckDimensions rejects dimension values no concrete request can
// carry: an empty string, the wildcard and the represented-user marker.
func validateCheckDimensions(c Check) error {
	for key, value := range c.Dimensions {
		switch value {
		case "":
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" is empty")
		case WildcardResource, SelectorValueRepresentedUser:
			return invalidCheckReason(c.Scope, c.ResourceID, "dimension "+key+" must be concrete")
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
			Check:    Check{Scope: exclusion, ResourceKind: check.ResourceKind, ResourceID: check.ResourceID, Dimensions: check.Dimensions, exact: true, representedUser: check.representedUser, probe: false, anyOf: nil, operation: check.operation},
			Instance: instance,
		},
	}
}

type RiskPolicyDimensions struct {
	ServerURL      string
	ServerIdentity string
}

// RiskPolicyEvaluateCheck asks whether the audience of a risk policy includes
// the caller. It names no server, and an audience grant limited to some
// servers still matches it: a match places the caller under the policy, so
// leaving the server out can only apply the policy more widely.
func RiskPolicyEvaluateCheck(policyID string) Check {
	return Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationPolicyAudience}
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
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, exact: true, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}

// InferenceExecuteCheck authorizes inference in one project, not chat mutation.
// It always carries project_id, so a project-bounded grant covers only its project.
func InferenceExecuteCheck(projectID string) Check {
	return Check{Scope: ScopeInferenceExecute, ResourceKind: "", ResourceID: projectID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
}

// RequirePluginWrite authorizes project-scoped plugin content mutations and
// publishing, without granting edits to the referenced skills or MCP servers.
// Keep the org-admin alternative for existing roles, whose grants are not
// rewritten when new default capabilities are introduced. Explicit exclusions
// still win over either allow alternative.
func (e *Engine) RequirePluginWrite(ctx context.Context, organizationID, projectID string) error {
	return e.RequireAnyUnblocked(ctx,
		Check{Scope: ScopePluginWrite, ResourceKind: ResourceKindProject, ResourceID: projectID, Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault},
		Check{Scope: ScopeOrgAdmin, ResourceKind: ResourceKindOrg, ResourceID: organizationID, Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault},
	)
}
