package authz

import "strings"

// Scope identifies an authorization capability granted on a resource.
type Scope string

type ScopeParts struct {
	Resource string
	Action   string
}

const (
	ScopeRoot                    Scope = "root"
	ScopeOrgRead                 Scope = "org:read"
	ScopeOrgBlockedRead          Scope = "org:blocked_read"
	ScopeOrgAdmin                Scope = "org:admin"
	ScopeOrgBlockedAdmin         Scope = "org:blocked_admin"
	ScopeProjectRead             Scope = "project:read"
	ScopeProjectBlockedRead      Scope = "project:blocked_read"
	ScopeProjectWrite            Scope = "project:write"
	ScopeProjectBlockedWrite     Scope = "project:blocked_write"
	ScopeMCPRead                 Scope = "mcp:read"
	ScopeMCPBlockedRead          Scope = "mcp:blocked_read"
	ScopeMCPWrite                Scope = "mcp:write"
	ScopeMCPBlockedWrite         Scope = "mcp:blocked_write"
	ScopeMCPConnect              Scope = "mcp:connect"
	ScopeMCPBlockedConnect       Scope = "mcp:blocked_connect"
	ScopeMCPAuth                 Scope = "mcp:auth"
	ScopeMCPBlockedAuth          Scope = "mcp:blocked_auth"
	ScopeEnvironmentRead         Scope = "environment:read"
	ScopeEnvironmentBlockedRead  Scope = "environment:blocked_read"
	ScopeEnvironmentWrite        Scope = "environment:write"
	ScopeEnvironmentBlockedWrite Scope = "environment:blocked_write"
	ScopeSkillRead               Scope = "skill:read"
	ScopeSkillBlockedRead        Scope = "skill:blocked_read"
	ScopeSkillWrite              Scope = "skill:write"
	ScopeSkillBlockedWrite       Scope = "skill:blocked_write"
	ScopePluginWrite             Scope = "plugin:write"
	ScopePluginBlockedWrite      Scope = "plugin:blocked_write"
	ScopeAssistantRead           Scope = "assistant:read"
	ScopeAssistantBlockedRead    Scope = "assistant:blocked_read"
	ScopeAssistantWrite          Scope = "assistant:write"
	ScopeAssistantBlockedWrite   Scope = "assistant:blocked_write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	ScopeRiskPolicyBlock         Scope = "risk_policy:block"
	ScopeChatRead                Scope = "chat:read"
	ScopeChatWrite               Scope = "chat:write"
	ScopeInferenceExecute        Scope = "inference:execute"
	ScopeLogRead                 Scope = "log:read"
	ScopeAgentRead               Scope = "agent:read"
	ScopeAgentWrite              Scope = "agent:write"
	ScopeAgentAuthorize          Scope = "agent:authorize"
	ScopeAgentTransfer           Scope = "agent:transfer"

	// Managing an organization's workload identity trust policy: its issuers, its
	// admitted subjects, and which agent each inherits its policy from. Presence in
	// the admitted set is itself the grant of machine access, so it gets a
	// permission that says so rather than riding a project or issuer scope whose
	// product meaning is something else.
	//
	// "workload" here names the resource being configured, not
	// urn.PrincipalTypeWorkload, which is also "workload" and names the machine
	// principal itself. See ResourceKindWorkload.
	ScopeWorkloadRead         Scope = "workload:read"
	ScopeWorkloadBlockedRead  Scope = "workload:blocked_read"
	ScopeWorkloadWrite        Scope = "workload:write"
	ScopeWorkloadBlockedWrite Scope = "workload:blocked_write"
	// Device-agent sync and hook ingestion for agent-principal keys. Human
	// callers reach these routes through transport scopes, not these grants.
	ScopeOrgDeviceAgentSync Scope = "org:device_agent_sync"
	ScopeOrgHooksIngest     Scope = "org:hooks_ingest"
)

type scopeVisibility int

const (
	scopeVisibilityUserVisible scopeVisibility = iota + 1
	scopeVisibilityInternal
)

const (
	ScopeVisibilityUserVisible = "user_visible"
	ScopeVisibilityInternal    = "internal"
)

func (s Scope) Parts() ScopeParts {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok {
		return ScopeParts{Resource: string(s), Action: ""}
	}

	return ScopeParts{Resource: resource, Action: action}
}
