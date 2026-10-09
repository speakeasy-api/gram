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
	return Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: toolsetID, Dimensions: dimensions, selectorMatch: selectorMatchNormal}
}

// MCPCheck builds a Check for an MCP scope (read/write/connect) with project_id
// injected as a dimension so project-scoped grants can match.
func MCPCheck(scope Scope, resourceID, projectID string) Check {
	var dimensions map[string]string
	if projectID != "" {
		dimensions = map[string]string{SelectorKeyProjectID: projectID}
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: selectorMatchNormal}
}

// EnvironmentLinkCheck builds the project-wide environment:read check that
// guards binding an environment to something a caller can invoke (a source, a
// toolset, an MCP server) or redirecting where a bound environment's values
// are sent. The grant must be project-wide: a wildcard grant confined by the
// project_id dimension (or, by scope expansion, environment:write) satisfies
// it; a grant naming a single environment does not. It does not see an
// exclusion naming one environment, so callers pair it with
// EnvironmentReadCheck for the environments actually involved; see
// EnvironmentLinkChecks.
func EnvironmentLinkCheck(projectID string) Check {
	return Check{Scope: ScopeEnvironmentRead, ResourceKind: "environment", ResourceID: projectID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal}
}

// EnvironmentReadCheck builds the environment:read check for one environment
// in projectID. Unlike EnvironmentLinkCheck it is refused by an exclusion
// naming that environment.
func EnvironmentReadCheck(environmentID, projectID string) Check {
	return Check{Scope: ScopeEnvironmentRead, ResourceKind: "environment", ResourceID: environmentID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal}
}

// EnvironmentLinkChecks builds the checks for linking, unlinking or moving the
// destination of the given environments: the project-wide EnvironmentLinkCheck
// plus EnvironmentReadCheck for each distinct environment, all of which must
// pass. The caller passes every environment the change affects, so an
// exclusion on any one of them refuses it while a link that involves only
// readable environments still passes.
func EnvironmentLinkChecks(projectID string, environmentIDs ...string) []Check {
	checks := []Check{EnvironmentLinkCheck(projectID)}
	seen := make(map[string]struct{}, len(environmentIDs))
	for _, id := range environmentIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		checks = append(checks, EnvironmentReadCheck(id, projectID))
	}
	return checks
}

// AssistantCheck builds a Check for an assistant scope. resourceID is the
// assistant ID for operations on one assistant, or the project ID for
// project-level operations such as creating an assistant. projectID is injected
// as a dimension, so a project-wide grant ({"resource_id":"*","project_id":P})
// covers every assistant in P, while a grant naming one assistant
// ({"resource_id":A}) covers only that assistant.
func AssistantCheck(scope Scope, resourceID, projectID string) Check {
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: map[string]string{SelectorKeyProjectID: projectID}, selectorMatch: selectorMatchNormal}
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
			Check:    Check{Scope: exclusion, ResourceKind: check.ResourceKind, ResourceID: check.ResourceID, Dimensions: check.Dimensions, selectorMatch: selectorMatchStrict},
			Instance: instance,
		},
	}
}

type RiskPolicyDimensions struct {
	ServerURL      string
	ServerIdentity string
}

func RiskPolicyEvaluateCheck(policyID string) Check {
	return Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil, selectorMatch: selectorMatchNormal}
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
	return Check{Scope: ScopeChatRead, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal}
}

// ChatWriteCheck builds a Check authorizing destructive mutation of agent chat
// sessions — renaming, attaching feedback, and deleting. It is deliberately
// separate from ChatReadCheck: a session reviewer granted chat:read can read
// every transcript (and pin one as a shared bookmark) but must not be able to
// destroy one. Owner-matching in the chat handlers still lets anyone mutate
// their own sessions without a grant.
func ChatWriteCheck(resourceID string) Check {
	return Check{Scope: ScopeChatWrite, ResourceKind: "", ResourceID: resourceID, Dimensions: nil, selectorMatch: selectorMatchNormal}
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
	return Check{Scope: ScopeRiskPolicyBypass, ResourceKind: "", ResourceID: policyID, Dimensions: dimensions, selectorMatch: selectorMatchStrict}
}
