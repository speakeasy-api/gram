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

// ScopeEntry defines one scope: what a person needs to know before granting
// it, and the rules the engine evaluates it by. Every table the engine,
// the API and the dashboard read about scopes is derived from these entries,
// so adding a scope means adding one entry here plus, where they apply, a role
// default and an agent runtime eligibility entry.
//
// The presentation fields are exported for callers that list or describe
// scopes. The evaluation rules are unexported so that only this package reads
// them, through ExclusionScopeFor, GrantSurfaceForScope, ScopeVisibilityFor,
// ResourceKindForScope and check expansion.
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

	// visibility says whether the scope is exposed to clients as a
	// first-class permission or only used to store and evaluate exceptions.
	visibility scopeVisibility

	// surface is the product surface that owns writes of grants for the
	// scope.
	surface GrantSurface

	// satisfiedBy lists the other scopes whose grants also satisfy a check
	// of this scope. For allow scopes these are higher-privilege scopes; for
	// exception scopes, broader exception scopes. Expansion is one step and
	// not transitive: list every satisfying scope directly.
	satisfiedBy []Scope

	// blockedBy is the exception scope that withdraws this scope from a
	// grant, or empty when nothing can. Broader exception scopes reach it
	// through their own satisfiedBy.
	blockedBy Scope
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
// bypass, never a grant a person reads about. See rootScopeEntry.
//
// A few rules are easy to break when adding an entry:
//
//   - environment:* scopes are independent of project:*, as mcp:* scopes are.
//     Environment checks carry their own resource kind with the project ID as
//     a dimension, so expansion across the boundary would never match a
//     selector, and project:read must not reveal environment values, which
//     include secrets. Roles that need environment access hold environment
//     scopes explicitly.
//   - The mcp:blocked_* scopes are independent of each other, unlike the other
//     exception families. Connecting to a server and administering it are
//     different jobs, so "cannot connect" must be expressible without also
//     meaning "cannot see or manage". Taking a principal off a server
//     entirely writes all three.
//   - mcp:auth is independent of every other scope: server access, authoring
//     and administration imply no use of an auth client.
var scopeCatalog = []ScopeEntry{
	{
		Scope:        ScopeOrgRead,
		Label:        "View organization",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "View organization settings and members.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeOrgAdmin},
		blockedBy:    ScopeOrgBlockedRead,
	},
	{
		Scope:        ScopeOrgBlockedRead,
		Label:        "Exception: view organization",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "Store exceptions for organization read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeOrgAdmin,
		Label:        "Manage organization",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "Manage organization settings, members, roles and access. Includes removing members and changing what every role can do.",
		Sensitivity:  SensitivityDestructiveAdmin,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeOrgBlockedAdmin,
	},
	{
		Scope:        ScopeOrgBlockedAdmin,
		Label:        "Exception: manage organization",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "Store exceptions for organization admin access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeOrgBlockedRead},
		blockedBy:    "",
	},
	{
		Scope:        ScopeOrgDeviceAgentSync,
		Label:        "Sync device agent plugins",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "Let an agent's device agent sync the plugins assigned to it.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeOrgAdmin},
		blockedBy:    "",
	},
	{
		Scope:        ScopeOrgHooksIngest,
		Label:        "Send hook events",
		Category:     ScopeCategoryOrg,
		ResourceType: ResourceKindOrg,
		Description:  "Let an agent send AI-tool hook events and telemetry.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeOrgAdmin},
		blockedBy:    "",
	},
	{
		Scope:        ScopeProjectRead,
		Label:        "View projects",
		Category:     ScopeCategoryProject,
		ResourceType: ResourceKindProject,
		Description:  "View projects and project-related resources.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeProjectWrite},
		blockedBy:    ScopeProjectBlockedRead,
	},
	{
		Scope:        ScopeProjectBlockedRead,
		Label:        "Exception: view projects",
		Category:     ScopeCategoryProject,
		ResourceType: ResourceKindProject,
		Description:  "Store exceptions for project read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeProjectWrite,
		Label:        "Manage projects",
		Category:     ScopeCategoryProject,
		ResourceType: ResourceKindProject,
		Description:  "Create and modify projects and project-related resources.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeProjectBlockedWrite,
	},
	{
		Scope:        ScopeProjectBlockedWrite,
		Label:        "Exception: manage projects",
		Category:     ScopeCategoryProject,
		ResourceType: ResourceKindProject,
		Description:  "Store exceptions for project write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeProjectBlockedRead},
		blockedBy:    "",
	},
	{
		Scope:        ScopeMCPRead,
		Label:        "View MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "View MCP servers and their configuration.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeMCPWrite},
		blockedBy:    ScopeMCPBlockedRead,
	},
	{
		Scope:        ScopeMCPBlockedRead,
		Label:        "Exception: view MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "Store exceptions for MCP read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeMCPWrite,
		Label:        "Manage MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "Create, change and delete MCP servers and their configuration.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeMCPBlockedWrite,
	},
	{
		Scope:        ScopeMCPBlockedWrite,
		Label:        "Exception: manage MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "Store exceptions for MCP write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeMCPConnect,
		Label:        "Connect to MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "Connect to MCP servers and call their tools.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeMCPRead, ScopeMCPWrite},
		blockedBy:    ScopeMCPBlockedConnect,
	},
	{
		Scope:        ScopeMCPBlockedConnect,
		Label:        "Exception: connect to MCP servers",
		Category:     ScopeCategoryMCP,
		ResourceType: ResourceKindMCP,
		Description:  "Store exceptions for MCP connect access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeMCPAuth,
		Label:        "Use shared auth clients",
		Category:     ScopeCategoryMCPAuthClient,
		ResourceType: ResourceKindMCPAuthClient,
		Description:  "Use an upstream auth client to reach MCP servers. Applies on every server that shares the client.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeMCPBlockedAuth,
	},
	{
		Scope:        ScopeMCPBlockedAuth,
		Label:        "Exception: use shared auth clients",
		Category:     ScopeCategoryMCPAuthClient,
		ResourceType: ResourceKindMCPAuthClient,
		Description:  "Store exceptions for auth client use.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeEnvironmentRead,
		Label:        "View environments",
		Category:     ScopeCategoryEnvironment,
		ResourceType: ResourceKindEnvironment,
		Description:  "View environments and their entries within the project, including secret values.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeEnvironmentWrite},
		blockedBy:    ScopeEnvironmentBlockedRead,
	},
	{
		Scope:        ScopeEnvironmentBlockedRead,
		Label:        "Exception: view environments",
		Category:     ScopeCategoryEnvironment,
		ResourceType: ResourceKindEnvironment,
		Description:  "Store exceptions for environment read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeEnvironmentWrite,
		Label:        "Manage environments",
		Category:     ScopeCategoryEnvironment,
		ResourceType: ResourceKindEnvironment,
		Description:  "Add, edit, clone, and remove environments within the project.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeEnvironmentBlockedWrite,
	},
	{
		Scope:        ScopeEnvironmentBlockedWrite,
		Label:        "Exception: manage environments",
		Category:     ScopeCategoryEnvironment,
		ResourceType: ResourceKindEnvironment,
		Description:  "Store exceptions for environment write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeEnvironmentBlockedRead},
		blockedBy:    "",
	},
	{
		Scope:        ScopeSkillRead,
		Label:        "View skills",
		Category:     ScopeCategorySkill,
		ResourceType: ResourceKindSkill,
		Description:  "View skills within the project.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeSkillWrite},
		blockedBy:    ScopeSkillBlockedRead,
	},
	{
		Scope:        ScopeSkillBlockedRead,
		Label:        "Exception: view skills",
		Category:     ScopeCategorySkill,
		ResourceType: ResourceKindSkill,
		Description:  "Store exceptions for skill read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeSkillWrite,
		Label:        "Manage skills",
		Category:     ScopeCategorySkill,
		ResourceType: ResourceKindSkill,
		Description:  "Create and modify skills within the project.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeSkillBlockedWrite,
	},
	{
		Scope:        ScopeSkillBlockedWrite,
		Label:        "Exception: manage skills",
		Category:     ScopeCategorySkill,
		ResourceType: ResourceKindSkill,
		Description:  "Store exceptions for skill write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeSkillBlockedRead},
		blockedBy:    "",
	},
	{
		Scope:        ScopePluginWrite,
		Label:        "Publish plugins",
		Category:     ScopeCategoryPlugin,
		ResourceType: ResourceKindProject,
		Description:  "Manage plugin contents and publish plugins within the project, without editing referenced skills or MCP servers.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopePluginBlockedWrite,
	},
	{
		Scope:        ScopePluginBlockedWrite,
		Label:        "Exception: publish plugins",
		Category:     ScopeCategoryPlugin,
		ResourceType: ResourceKindProject,
		Description:  "Store exceptions for plugin write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAssistantRead,
		Label:        "View assistants",
		Category:     ScopeCategoryAssistant,
		ResourceType: ResourceKindAssistant,
		Description:  "View and interact with assistants within the project.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeAssistantWrite},
		blockedBy:    ScopeAssistantBlockedRead,
	},
	{
		Scope:        ScopeAssistantBlockedRead,
		Label:        "Exception: view assistants",
		Category:     ScopeCategoryAssistant,
		ResourceType: ResourceKindAssistant,
		Description:  "Store exceptions for assistant read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAssistantWrite,
		Label:        "Manage assistants",
		Category:     ScopeCategoryAssistant,
		ResourceType: ResourceKindAssistant,
		Description:  "Create and modify assistants within the project.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeAssistantBlockedWrite,
	},
	{
		Scope:        ScopeAssistantBlockedWrite,
		Label:        "Exception: manage assistants",
		Category:     ScopeCategoryAssistant,
		ResourceType: ResourceKindAssistant,
		Description:  "Store exceptions for assistant write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeAssistantBlockedRead},
		blockedBy:    "",
	},
	{
		Scope:        ScopeRiskPolicyEvaluate,
		Label:        "Evaluate risk policies",
		Category:     ScopeCategoryRiskPolicy,
		ResourceType: ResourceKindRiskPolicy,
		Description:  "Put the holder in the named risk policy's audience, so the policy is evaluated against their requests unless a bypass grant for the same policy exempts the request.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceRiskPolicy,
		satisfiedBy:  nil,
		blockedBy:    ScopeRiskPolicyBypass,
	},
	{
		Scope:        ScopeRiskPolicyBypass,
		Label:        "Bypass risk policies",
		Category:     ScopeCategoryRiskPolicy,
		ResourceType: ResourceKindRiskPolicy,
		Description:  "Exempt the holder from the named risk policy for the exact server the grant names, by URL or identity. It only subtracts from a matching evaluate grant and never puts anyone in a policy's audience.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceRiskPolicy,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeRiskPolicyBlock,
		Label:        "Block shadow MCP servers",
		Category:     ScopeCategoryRiskPolicy,
		ResourceType: ResourceKindRiskPolicy,
		Description:  "Block specific shadow MCP servers under allow-by-default risk policies.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceRiskPolicy,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeChatRead,
		Label:        "View agent sessions",
		Category:     ScopeCategorySession,
		ResourceType: ResourceKindChat,
		Description:  "Read and pin other members' agent session transcripts, and reveal secrets flagged in Risk Events. Everyone keeps their own sessions.",
		Sensitivity:  SensitivityConversations,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeChatWrite},
		blockedBy:    "",
	},
	{
		Scope:        ScopeChatWrite,
		Label:        "Manage agent sessions",
		Category:     ScopeCategorySession,
		ResourceType: ResourceKindChat,
		Description:  "Rename, delete, and give feedback on other members' agent sessions, and read their transcripts. Everyone keeps their own.",
		Sensitivity:  SensitivityConversations,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeInferenceExecute,
		Label:        "Run inference",
		Category:     ScopeCategoryInference,
		ResourceType: ResourceKindInference,
		Description:  "Run model inference without accessing chat transcripts.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeLogRead,
		Label:        "View tool logs",
		Category:     ScopeCategoryLog,
		ResourceType: ResourceKindProject,
		Description:  "View the project's tool-call logs and traces, including tool arguments, results, and prompt text from every member. Not implied by project or organization admin access.",
		Sensitivity:  SensitivityToolLogs,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAgentRead,
		Label:        "View agents",
		Category:     ScopeCategoryAgent,
		ResourceType: ResourceKindAgent,
		Description:  "View agents and their settings.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAgentWrite,
		Label:        "Manage agents",
		Category:     ScopeCategoryAgent,
		ResourceType: ResourceKindAgent,
		Description:  "Create, configure, and manage agents.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAgentAuthorize,
		Label:        "Authorize agent credentials",
		Category:     ScopeCategoryAgent,
		ResourceType: ResourceKindAgent,
		Description:  "Authorize agents and manage their credentials.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeAgentTransfer,
		Label:        "Transfer agent ownership",
		Category:     ScopeCategoryAgent,
		ResourceType: ResourceKindAgent,
		Description:  "Hand an agent to another owner. The previous owner loses control of it.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeWorkloadRead,
		Label:        "View workload identities",
		Category:     ScopeCategoryWorkload,
		ResourceType: ResourceKindWorkload,
		Description:  "View workload identity trust policy: issuers, admitted subjects, and assigned agents.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeWorkloadWrite},
		blockedBy:    ScopeWorkloadBlockedRead,
	},
	{
		Scope:        ScopeWorkloadBlockedRead,
		Label:        "Exception: view workload identities",
		Category:     ScopeCategoryWorkload,
		ResourceType: ResourceKindWorkload,
		Description:  "Store exceptions for workload read access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    "",
	},
	{
		Scope:        ScopeWorkloadWrite,
		Label:        "Manage workload identities",
		Category:     ScopeCategoryWorkload,
		ResourceType: ResourceKindWorkload,
		Description:  "Register workload issuers, admit and withdraw subjects, and assign agents to workloads.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityUserVisible,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  nil,
		blockedBy:    ScopeWorkloadBlockedWrite,
	},
	{
		Scope:        ScopeWorkloadBlockedWrite,
		Label:        "Exception: manage workload identities",
		Category:     ScopeCategoryWorkload,
		ResourceType: ResourceKindWorkload,
		Description:  "Store exceptions for workload write access.",
		Sensitivity:  SensitivityNone,
		visibility:   scopeVisibilityInternal,
		surface:      GrantSurfaceAccess,
		satisfiedBy:  []Scope{ScopeWorkloadBlockedRead},
		blockedBy:    "",
	},
}

// rootScopeEntry defines the engine's bypass scope. It is registered so that
// stored root grants evaluate, but it is internal and never catalogued.
var rootScopeEntry = ScopeEntry{
	Scope:        ScopeRoot,
	Label:        "",
	Category:     "",
	ResourceType: ResourceKindWildcard,
	Description:  "",
	Sensitivity:  SensitivityNone,
	visibility:   scopeVisibilityInternal,
	surface:      GrantSurfaceAccess,
	satisfiedBy:  nil,
	blockedBy:    "",
}

// scopeDefinitions is every scope the engine knows: the catalogue plus root.
var scopeDefinitions = append([]ScopeEntry{rootScopeEntry}, scopeCatalog...)

// The lookup tables below are indexes over scopeDefinitions. Edit the
// entries above, never these.
var (
	scopeVisibilityByScope = indexScopes(func(e ScopeEntry) scopeVisibility { return e.visibility })
	scopeExpansions        = indexScopes(func(e ScopeEntry) []Scope { return e.satisfiedBy })
	scopeExclusions        = indexScopes(func(e ScopeEntry) Scope { return e.blockedBy })
	scopeGrantSurfaces     = indexScopes(func(e ScopeEntry) GrantSurface { return e.surface })
	scopeResourceKinds     = indexScopes(func(e ScopeEntry) string { return e.ResourceType })
)

func indexScopes[V any](field func(ScopeEntry) V) map[Scope]V {
	out := make(map[Scope]V, len(scopeDefinitions))
	for _, entry := range scopeDefinitions {
		out[entry.Scope] = field(entry)
	}
	return out
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

// ScopeNames returns every catalogued scope slug in presentation order. The
// API design uses it for its scope enums so a new scope needs no edit there.
func ScopeNames() []string {
	out := make([]string, 0, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		out = append(out, string(entry.Scope))
	}
	return out
}

// ExclusionScopeNames returns, in presentation order of the scopes they
// store exceptions for, every scope that stores exception grants.
func ExclusionScopeNames() []string {
	var out []string
	for _, entry := range scopeCatalog {
		exclusion, ok := ExclusionScopeFor(entry.Scope)
		if ok && !slices.Contains(out, string(exclusion)) {
			out = append(out, string(exclusion))
		}
	}
	return out
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
