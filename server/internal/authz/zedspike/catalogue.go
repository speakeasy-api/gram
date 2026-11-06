//nolint:exhaustruct // Catalogue entries list only the fields that differ from the zero value.
package zedspike

// Scope is a stored grant scope, as written in principal_grants.scope.
type Scope string

const (
	ScopeRoot                    Scope = "root"
	ScopeOrgRead                 Scope = "org:read"
	ScopeOrgBlockedRead          Scope = "org:blocked_read"
	ScopeOrgAdmin                Scope = "org:admin"
	ScopeOrgBlockedAdmin         Scope = "org:blocked_admin"
	ScopeOrgDeviceAgentSync      Scope = "org:device_agent_sync"
	ScopeOrgHooksIngest          Scope = "org:hooks_ingest"
	ScopeProjectRead             Scope = "project:read"
	ScopeProjectBlockedRead      Scope = "project:blocked_read"
	ScopeProjectWrite            Scope = "project:write"
	ScopeProjectBlockedWrite     Scope = "project:blocked_write"
	ScopePluginWrite             Scope = "plugin:write"
	ScopePluginBlockedWrite      Scope = "plugin:blocked_write"
	ScopeLogRead                 Scope = "log:read"
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
	ScopeAssistantRead           Scope = "assistant:read"
	ScopeAssistantBlockedRead    Scope = "assistant:blocked_read"
	ScopeAssistantWrite          Scope = "assistant:write"
	ScopeAssistantBlockedWrite   Scope = "assistant:blocked_write"
	ScopeInferenceExecute        Scope = "inference:execute"
	ScopeChatRead                Scope = "chat:read"
	ScopeChatWrite               Scope = "chat:write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	ScopeRiskPolicyBlock         Scope = "risk_policy:block"
)

// Selector keys a grant row may carry beyond resource_kind and resource_id.
const (
	SelectorProjectID       = "project_id"
	SelectorTool            = "tool"
	SelectorDisposition     = "disposition"
	SelectorClientType      = "client_type"
	SelectorConnectionOwner = "connection_owner"
	SelectorUpstreamClient  = "upstream_client"
	SelectorServerURL       = "server_url"
	SelectorServerIdentity  = "server_identity"
)

// Surface is the product surface that writes a scope's grants.
type Surface string

const (
	SurfaceAccess     Surface = "access"
	SurfaceRiskPolicy Surface = "risk_policy"
)

// ScopeInfo is what a stored scope needs beyond the schema graph.
type ScopeInfo struct {
	Description string
	// Sensitive scopes are never granted by default and are flagged in the
	// role editor.
	Sensitive bool
	// Strict scopes match every selector key exactly, allows included.
	Strict bool
	// Block marks a *:blocked_* scope.
	Block bool
	// DelegatableSince is the runtime scope registry version from which an
	// owner may delegate the scope to an agent; 0 means never. It is checked
	// when an agent policy is written, not at check time: an administrator
	// can still write any scope directly to an agent, so every relation lists
	// agent as a subject.
	DelegatableSince int
	// Selectors are the keys a row may carry besides resource_kind and
	// resource_id. Each must be a project edge or a caveat's granted_ parameter.
	Selectors []string
	// Surface writes the scope's grants. Empty means SurfaceAccess.
	Surface Surface
}

var (
	mcpSelectors        = []string{SelectorProjectID, SelectorTool, SelectorDisposition}
	authClientSelectors = []string{SelectorClientType, SelectorConnectionOwner, SelectorProjectID, SelectorUpstreamClient}
	riskSelectors       = []string{SelectorServerIdentity, SelectorServerURL}
	projectSelector     = []string{SelectorProjectID}
)

func block(since int, selectors ...string) ScopeInfo {
	return ScopeInfo{Block: true, DelegatableSince: since, Selectors: selectors}
}

// ScopeCatalogue holds every scope the schema names. The loader fails when a
// relation names a scope that is missing here, or a scope here has no
// relation.
var ScopeCatalogue = map[Scope]ScopeInfo{
	// Candidate for deletion: see the README.
	ScopeRoot: {Description: "Every grant except upstream auth clients"},

	ScopeOrgRead:            {Description: "Read the organization"},
	ScopeOrgAdmin:           {Description: "Administer the organization"},
	ScopeOrgDeviceAgentSync: {Description: "Sync device agents", DelegatableSince: 2},
	ScopeOrgHooksIngest:     {Description: "Ingest hook events", Selectors: projectSelector, DelegatableSince: 2},
	ScopeProjectRead:        {Description: "Read the project", DelegatableSince: 1},
	ScopeProjectWrite:       {Description: "Change the project", DelegatableSince: 1},
	ScopePluginWrite:        {Description: "Publish plugins"},
	ScopeLogRead:            {Description: "Read tool-call logs", Sensitive: true},
	ScopeMCPRead:            {Description: "Inspect server configuration", Selectors: mcpSelectors, DelegatableSince: 1},
	ScopeMCPWrite:           {Description: "Change server configuration", Selectors: mcpSelectors, DelegatableSince: 1},
	ScopeMCPConnect:         {Description: "Connect and invoke permitted tools", Selectors: mcpSelectors, DelegatableSince: 1},
	ScopeMCPAuth:            {Description: "Use an upstream auth client", Selectors: authClientSelectors, Strict: true, DelegatableSince: 5},
	ScopeEnvironmentRead:    {Description: "View environments and their entries", Selectors: projectSelector, DelegatableSince: 1},
	ScopeEnvironmentWrite:   {Description: "Add, edit, clone and remove environments", Selectors: projectSelector, DelegatableSince: 1},
	ScopeSkillRead:          {Description: "Read skills", Selectors: projectSelector, DelegatableSince: 1},
	ScopeSkillWrite:         {Description: "Change skills", Selectors: projectSelector, DelegatableSince: 1},
	ScopeAssistantRead:      {Description: "Read assistants", Selectors: projectSelector, DelegatableSince: 3},
	ScopeAssistantWrite:     {Description: "Change assistants", Selectors: projectSelector, DelegatableSince: 3},
	ScopeInferenceExecute:   {Description: "Run inference through Speakeasy as a workload", Selectors: projectSelector, DelegatableSince: 6},
	ScopeChatRead:           {Description: "Read session transcripts", Selectors: projectSelector, Sensitive: true},
	ScopeChatWrite:          {Description: "Change session transcripts", Selectors: projectSelector},
	ScopeRiskPolicyEvaluate: {Description: "Apply the policy", Selectors: riskSelectors, Surface: SurfaceRiskPolicy, DelegatableSince: 1},
	ScopeRiskPolicyBypass:   {Description: "Skip the policy", Selectors: riskSelectors, Surface: SurfaceRiskPolicy},
	ScopeRiskPolicyBlock:    {Description: "Block with the policy", Selectors: riskSelectors, Surface: SurfaceRiskPolicy},

	ScopeOrgBlockedRead:          block(0),
	ScopeOrgBlockedAdmin:         block(0),
	ScopeProjectBlockedRead:      block(4),
	ScopeProjectBlockedWrite:     block(4),
	ScopePluginBlockedWrite:      block(0),
	ScopeMCPBlockedRead:          block(4, mcpSelectors...),
	ScopeMCPBlockedWrite:         block(4, mcpSelectors...),
	ScopeMCPBlockedConnect:       block(4, mcpSelectors...),
	ScopeMCPBlockedAuth:          {Block: true, Strict: true, DelegatableSince: 5, Selectors: authClientSelectors},
	ScopeEnvironmentBlockedRead:  block(4, projectSelector...),
	ScopeEnvironmentBlockedWrite: block(4, projectSelector...),
	ScopeSkillBlockedRead:        block(4, projectSelector...),
	ScopeSkillBlockedWrite:       block(4, projectSelector...),
	ScopeAssistantBlockedRead:    block(4, projectSelector...),
	ScopeAssistantBlockedWrite:   block(4, projectSelector...),
}

// UndeclaredScopes are stored scopes whose resource types the schema does not
// declare yet. Their checks keep running through the engine against the
// scope tables, exactly as today.
var UndeclaredScopes = []Scope{
	"agent:read", "agent:write", "agent:authorize", "agent:transfer",
	"workload:read", "workload:blocked_read", "workload:write", "workload:blocked_write",
}

// Answers names the stored scope each public permission answers in the old
// scope tables, which stay authoritative until the release gate. It is
// written out rather than derived from the expression, so reordering a union
// in rbac.zed never changes the old tables.
var Answers = map[PermissionKey]Scope{
	{"organization", "read"}:              ScopeOrgRead,
	{"organization", "administer"}:        ScopeOrgAdmin,
	{"organization", "sync_device_agent"}: ScopeOrgDeviceAgentSync,
	{"organization", "ingest_hooks"}:      ScopeOrgHooksIngest,
	{"project", "read"}:                   ScopeProjectRead,
	{"project", "write"}:                  ScopeProjectWrite,
	{"project", "publish_plugin"}:         ScopePluginWrite,
	{"project", "read_logs"}:              ScopeLogRead,
	{"project", "read_mcp_servers"}:       ScopeMCPRead,
	{"project", "create_mcp_server"}:      ScopeMCPWrite,
	{"project", "read_environments"}:      ScopeEnvironmentRead,
	{"project", "create_environment"}:     ScopeEnvironmentWrite,
	{"project", "read_skills"}:            ScopeSkillRead,
	{"project", "write_skills"}:           ScopeSkillWrite,
	{"project", "read_assistants"}:        ScopeAssistantRead,
	{"project", "create_assistant"}:       ScopeAssistantWrite,
	{"project", "read_chats"}:             ScopeChatRead,
	{"mcp_server", "read"}:                ScopeMCPRead,
	{"mcp_server", "edit"}:                ScopeMCPWrite,
	{"mcp_server", "connect"}:             ScopeMCPConnect,
	{"mcp_auth_client", "use"}:            ScopeMCPAuth,
	{"environment", "read"}:               ScopeEnvironmentRead,
	{"environment", "write"}:              ScopeEnvironmentWrite,
	{"skill", "read"}:                     ScopeSkillRead,
	{"skill", "write"}:                    ScopeSkillWrite,
	{"assistant", "read"}:                 ScopeAssistantRead,
	{"assistant", "write"}:                ScopeAssistantWrite,
	{"inference", "execute"}:              ScopeInferenceExecute,
	{"chat", "read_grant"}:                ScopeChatRead,
	{"chat", "write_grant"}:               ScopeChatWrite,
	{"risk_policy", "evaluate"}:           ScopeRiskPolicyEvaluate,
	{"risk_policy", "bypass"}:             ScopeRiskPolicyBypass,
	{"risk_policy", "block"}:              ScopeRiskPolicyBlock,
}

// Composites are public permissions that answer no single scope: either of
// two permissions allows. The old tables list them as "any of". Each one
// names the scopes its branches answer, so the table check can require every
// block of those scopes on the subtracted side, as it does for Answers.
var Composites = map[PermissionKey][]Scope{
	{"project", "plugin_publication"}:        {ScopePluginWrite},
	{"project", "read_or_organization_read"}: {ScopeProjectRead, ScopeOrgRead},
	{"project", "write_or_administer"}:       {ScopeProjectWrite},
	{"mcp_server", "project_read_or_edit"}:   {ScopeProjectRead, ScopeMCPWrite},
	{"chat", "read"}:                         {ScopeChatRead},
	{"chat", "write"}:                        {ScopeChatWrite},
	{"agent", "is_owner"}:                    nil,
}

// PermissionKey names a permission on a type.
type PermissionKey struct {
	Type       string
	Permission string
}
