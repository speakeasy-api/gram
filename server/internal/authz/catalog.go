package authz

import "slices"

// Sensitivity says why granting a scope deserves a warning. The empty value
// marks a scope that needs none.
type Sensitivity string

const (
	// SensitivityNone marks a scope that needs no warning before it is granted.
	SensitivityNone Sensitivity = ""

	// SensitivityConversations marks a scope that exposes other members'
	// agent session transcripts.
	SensitivityConversations Sensitivity = "conversations"

	// SensitivityToolLogs marks a scope that exposes tool arguments, results
	// and prompt text from every member of a project.
	SensitivityToolLogs Sensitivity = "tool_logs"

	// SensitivityDestructiveAdmin marks a scope that can remove members or
	// change what every role can do.
	SensitivityDestructiveAdmin Sensitivity = "destructive_admin"
)

// ScopeCategory groups scopes the way the dashboard presents them. It differs
// from the resource kind a check carries where one resource kind holds several
// permission families: plugin:write and log:read are both checked against a
// project, but they are not project permissions to a person reading a role.
type ScopeCategory string

const (
	ScopeCategoryOrg           ScopeCategory = "org"
	ScopeCategoryProject       ScopeCategory = "project"
	ScopeCategoryMCP           ScopeCategory = "mcp"
	ScopeCategoryMCPAuthClient ScopeCategory = "mcp_auth_client"
	ScopeCategoryEnvironment   ScopeCategory = "environment"
	ScopeCategorySkill         ScopeCategory = "skill"
	ScopeCategoryPlugin        ScopeCategory = "plugin"
	ScopeCategoryAssistant     ScopeCategory = "assistant"
	ScopeCategoryRiskPolicy    ScopeCategory = "risk_policy"
	ScopeCategorySession       ScopeCategory = "session"
	ScopeCategoryInference     ScopeCategory = "inference"
	ScopeCategoryLog           ScopeCategory = "log"
	ScopeCategoryAgent         ScopeCategory = "agent"
	ScopeCategoryWorkload      ScopeCategory = "workload"
)

// ScopeCategories lists every category in presentation order.
var ScopeCategories = []ScopeCategory{
	ScopeCategoryOrg,
	ScopeCategoryProject,
	ScopeCategoryMCP,
	ScopeCategoryMCPAuthClient,
	ScopeCategoryEnvironment,
	ScopeCategorySkill,
	ScopeCategoryPlugin,
	ScopeCategoryAssistant,
	ScopeCategoryRiskPolicy,
	ScopeCategorySession,
	ScopeCategoryInference,
	ScopeCategoryLog,
	ScopeCategoryAgent,
	ScopeCategoryWorkload,
}

// ScopeEntry is what a person needs to know about one scope before granting
// it: a short name, what it allows, where it sits in the dashboard, and
// whether it deserves a warning. The authorization semantics (expansions,
// exclusions, visibility, agent eligibility) stay in scopes.go and the agent
// runtime registry; the catalogue only describes them.
type ScopeEntry struct {
	Scope Scope

	// Label is the short name shown in place of the slug, e.g. "View tool
	// logs".
	Label string

	// Category is the dashboard grouping.
	Category ScopeCategory

	// ResourceType is the resource kind the scope is checked against. It is
	// what selectors on a grant of this scope refer to.
	ResourceType string

	// Description says what a holder can do and, where it matters, what the
	// scope does not include.
	Description string

	// Sensitivity says why the scope deserves a warning before it is granted.
	Sensitivity Sensitivity
}

// Sensitive reports whether granting the scope deserves a warning.
func (e ScopeEntry) Sensitive() bool {
	return e.Sensitivity != SensitivityNone
}

// SubScopes lists the user-visible scopes a grant of this scope also
// satisfies, one step down the expansion graph.
func (e ScopeEntry) SubScopes() []string {
	return CalculateSubScopes(e.Scope)
}

// scopeCatalog lists every scope a role can hold, in the order the catalogue
// presents them. ScopeRoot is deliberately absent: it is the engine's own
// bypass, never a grant a person reads about.
var scopeCatalog = []ScopeEntry{
	{Scope: ScopeOrgRead, Label: "View organization", Category: ScopeCategoryOrg, ResourceType: "org", Description: "View organization settings and members.", Sensitivity: SensitivityNone},
	{Scope: ScopeOrgBlockedRead, Label: "Blocked: view organization", Category: ScopeCategoryOrg, ResourceType: "org", Description: "Store exceptions for organization read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeOrgAdmin, Label: "Manage organization", Category: ScopeCategoryOrg, ResourceType: "org", Description: "Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.", Sensitivity: SensitivityDestructiveAdmin},
	{Scope: ScopeOrgBlockedAdmin, Label: "Blocked: manage organization", Category: ScopeCategoryOrg, ResourceType: "org", Description: "Store exceptions for organization admin access.", Sensitivity: SensitivityNone},
	{Scope: ScopeOrgDeviceAgentSync, Label: "Sync device agent plugins", Category: ScopeCategoryOrg, ResourceType: "org", Description: "Let an agent's device agent sync the plugins assigned to it.", Sensitivity: SensitivityNone},
	{Scope: ScopeOrgHooksIngest, Label: "Send hook events", Category: ScopeCategoryOrg, ResourceType: "org", Description: "Let an agent send AI-tool hook events and telemetry.", Sensitivity: SensitivityNone},
	{Scope: ScopeProjectRead, Label: "View projects", Category: ScopeCategoryProject, ResourceType: "project", Description: "View projects and project-related resources.", Sensitivity: SensitivityNone},
	{Scope: ScopeProjectBlockedRead, Label: "Blocked: view projects", Category: ScopeCategoryProject, ResourceType: "project", Description: "Store exceptions for project read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeProjectWrite, Label: "Manage projects", Category: ScopeCategoryProject, ResourceType: "project", Description: "Create and modify projects and project-related resources.", Sensitivity: SensitivityNone},
	{Scope: ScopeProjectBlockedWrite, Label: "Blocked: manage projects", Category: ScopeCategoryProject, ResourceType: "project", Description: "Store exceptions for project write access.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPRead, Label: "View MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "View MCP servers and their configuration.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPBlockedRead, Label: "Blocked: view MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "Store exceptions for MCP read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPWrite, Label: "Manage MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "Create, change and delete MCP servers and their configuration.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPBlockedWrite, Label: "Blocked: manage MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "Store exceptions for MCP write access.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPConnect, Label: "Connect to MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "Connect to MCP servers and call their tools.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPBlockedConnect, Label: "Blocked: connect to MCP servers", Category: ScopeCategoryMCP, ResourceType: "mcp", Description: "Store exceptions for MCP connect access.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPAuth, Label: "Use shared auth clients", Category: ScopeCategoryMCPAuthClient, ResourceType: "mcp_auth_client", Description: "Use an upstream auth client to reach MCP servers. Applies on every server that shares the client.", Sensitivity: SensitivityNone},
	{Scope: ScopeMCPBlockedAuth, Label: "Blocked: use shared auth clients", Category: ScopeCategoryMCPAuthClient, ResourceType: "mcp_auth_client", Description: "Store exceptions for auth client use.", Sensitivity: SensitivityNone},
	{Scope: ScopeEnvironmentRead, Label: "View environments", Category: ScopeCategoryEnvironment, ResourceType: "environment", Description: "View environments and their entries within the project, including secret values.", Sensitivity: SensitivityNone},
	{Scope: ScopeEnvironmentBlockedRead, Label: "Blocked: view environments", Category: ScopeCategoryEnvironment, ResourceType: "environment", Description: "Store exceptions for environment read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeEnvironmentWrite, Label: "Manage environments", Category: ScopeCategoryEnvironment, ResourceType: "environment", Description: "Add, edit, clone, and remove environments within the project.", Sensitivity: SensitivityNone},
	{Scope: ScopeEnvironmentBlockedWrite, Label: "Blocked: manage environments", Category: ScopeCategoryEnvironment, ResourceType: "environment", Description: "Store exceptions for environment write access.", Sensitivity: SensitivityNone},
	{Scope: ScopeSkillRead, Label: "View skills", Category: ScopeCategorySkill, ResourceType: "skill", Description: "View skills within the project.", Sensitivity: SensitivityNone},
	{Scope: ScopeSkillBlockedRead, Label: "Blocked: view skills", Category: ScopeCategorySkill, ResourceType: "skill", Description: "Store exceptions for skill read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeSkillWrite, Label: "Manage skills", Category: ScopeCategorySkill, ResourceType: "skill", Description: "Create and modify skills within the project.", Sensitivity: SensitivityNone},
	{Scope: ScopeSkillBlockedWrite, Label: "Blocked: manage skills", Category: ScopeCategorySkill, ResourceType: "skill", Description: "Store exceptions for skill write access.", Sensitivity: SensitivityNone},
	{Scope: ScopePluginWrite, Label: "Publish plugins", Category: ScopeCategoryPlugin, ResourceType: "project", Description: "Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.", Sensitivity: SensitivityNone},
	{Scope: ScopePluginBlockedWrite, Label: "Blocked: publish plugins", Category: ScopeCategoryPlugin, ResourceType: "project", Description: "Store exceptions for plugin write access.", Sensitivity: SensitivityNone},
	{Scope: ScopeAssistantRead, Label: "View assistants", Category: ScopeCategoryAssistant, ResourceType: "assistant", Description: "View and interact with assistants within the project.", Sensitivity: SensitivityNone},
	{Scope: ScopeAssistantBlockedRead, Label: "Blocked: view assistants", Category: ScopeCategoryAssistant, ResourceType: "assistant", Description: "Store exceptions for assistant read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeAssistantWrite, Label: "Manage assistants", Category: ScopeCategoryAssistant, ResourceType: "assistant", Description: "Create and modify assistants within the project.", Sensitivity: SensitivityNone},
	{Scope: ScopeAssistantBlockedWrite, Label: "Blocked: manage assistants", Category: ScopeCategoryAssistant, ResourceType: "assistant", Description: "Store exceptions for assistant write access.", Sensitivity: SensitivityNone},
	{Scope: ScopeRiskPolicyEvaluate, Label: "Evaluate risk policies", Category: ScopeCategoryRiskPolicy, ResourceType: "risk_policy", Description: "Put the holder in the named risk policy's audience, so the policy is evaluated against their requests unless a bypass grant for the same policy exempts the request.", Sensitivity: SensitivityNone},
	{Scope: ScopeRiskPolicyBypass, Label: "Bypass risk policies", Category: ScopeCategoryRiskPolicy, ResourceType: "risk_policy", Description: "Exempt the holder from the named risk policy for the exact server the grant names, by URL or identity. It only subtracts from a matching evaluate grant and never puts anyone in a policy's audience.", Sensitivity: SensitivityNone},
	{Scope: ScopeRiskPolicyBlock, Label: "Block shadow MCP servers", Category: ScopeCategoryRiskPolicy, ResourceType: "risk_policy", Description: "Block specific shadow MCP servers under allow-by-default risk policies.", Sensitivity: SensitivityNone},
	{Scope: ScopeChatRead, Label: "View agent sessions", Category: ScopeCategorySession, ResourceType: "chat", Description: "Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.", Sensitivity: SensitivityConversations},
	{Scope: ScopeChatWrite, Label: "Manage agent sessions", Category: ScopeCategorySession, ResourceType: "chat", Description: "Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.", Sensitivity: SensitivityConversations},
	{Scope: ScopeInferenceExecute, Label: "Run inference", Category: ScopeCategoryInference, ResourceType: "inference", Description: "Run model inference without accessing chat transcripts.", Sensitivity: SensitivityNone},
	{Scope: ScopeLogRead, Label: "View tool logs", Category: ScopeCategoryLog, ResourceType: "project", Description: "View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.", Sensitivity: SensitivityToolLogs},
	{Scope: ScopeAgentRead, Label: "View agents", Category: ScopeCategoryAgent, ResourceType: "agent", Description: "View agents and their settings.", Sensitivity: SensitivityNone},
	{Scope: ScopeAgentWrite, Label: "Manage agents", Category: ScopeCategoryAgent, ResourceType: "agent", Description: "Create, configure, and manage agents.", Sensitivity: SensitivityNone},
	{Scope: ScopeAgentAuthorize, Label: "Authorize agent credentials", Category: ScopeCategoryAgent, ResourceType: "agent", Description: "Authorize agents and manage their credentials.", Sensitivity: SensitivityNone},
	{Scope: ScopeAgentTransfer, Label: "Transfer agent ownership", Category: ScopeCategoryAgent, ResourceType: "agent", Description: "Hand an agent to another owner. The previous owner loses control of it.", Sensitivity: SensitivityNone},
	{Scope: ScopeWorkloadRead, Label: "View workload identities", Category: ScopeCategoryWorkload, ResourceType: "workload", Description: "View workload identity trust policy: issuers, admitted subjects, and assigned agents.", Sensitivity: SensitivityNone},
	{Scope: ScopeWorkloadBlockedRead, Label: "Blocked: view workload identities", Category: ScopeCategoryWorkload, ResourceType: "workload", Description: "Store exceptions for workload read access.", Sensitivity: SensitivityNone},
	{Scope: ScopeWorkloadWrite, Label: "Manage workload identities", Category: ScopeCategoryWorkload, ResourceType: "workload", Description: "Register workload issuers, admit and withdraw subjects, and assign agents to workloads.", Sensitivity: SensitivityNone},
	{Scope: ScopeWorkloadBlockedWrite, Label: "Blocked: manage workload identities", Category: ScopeCategoryWorkload, ResourceType: "workload", Description: "Store exceptions for workload write access.", Sensitivity: SensitivityNone},
}

var scopeCatalogByScope = func() map[Scope]ScopeEntry {
	out := make(map[Scope]ScopeEntry, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		out[entry.Scope] = entry
	}
	return out
}()

// ScopeCatalog returns every grantable scope with its presentation metadata,
// in presentation order. The slice is a copy; callers may reorder it.
func ScopeCatalog() []ScopeEntry {
	return slices.Clone(scopeCatalog)
}

// ScopeEntryFor returns the catalogue entry for scope.
func ScopeEntryFor(scope Scope) (ScopeEntry, bool) {
	entry, ok := scopeCatalogByScope[scope]
	return entry, ok
}

// SensitiveScopes returns, in presentation order, the catalogue scopes that
// deserve a warning before they are granted.
func SensitiveScopes() []Scope {
	var out []Scope
	for _, entry := range scopeCatalog {
		if entry.Sensitive() {
			out = append(out, entry.Scope)
		}
	}
	return out
}
