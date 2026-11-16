// The building blocks of a scope: resource kinds, the closed list of actions,
// and the categories scopes are presented under.
//
// authzgen copies everything below the package line into package authz, so
// the model and the evaluator share these names without importing each other.
// Keep this file free of imports and of anything only the registry needs.
package registry

// Kind names a resource type, for example "mcp". It is the part of a scope
// before the colon.
type Kind string

// Action is what a permission allows on its resource type. It is the part of
// a scope after the colon, so Actions is every verb a new scope can end in.
type Action string

const (
	ActionRead      Action = "read"
	ActionWrite     Action = "write"
	ActionAdmin     Action = "admin"
	ActionConnect   Action = "connect"
	ActionAuth      Action = "auth"
	ActionExecute   Action = "execute"
	ActionEvaluate  Action = "evaluate"
	ActionBypass    Action = "bypass"
	ActionBlock     Action = "block"
	ActionAuthorize Action = "authorize"
	ActionTransfer  Action = "transfer"
)

// Actions is the closed list of actions a permission may declare. A new verb
// is added here first; Validate refuses a permission whose action is missing.
var Actions = []Action{
	ActionRead,
	ActionWrite,
	ActionAdmin,
	ActionConnect,
	ActionAuth,
	ActionExecute,
	ActionEvaluate,
	ActionBypass,
	ActionBlock,
	ActionAuthorize,
	ActionTransfer,
}

// Actions that are not a single verb. They predate the closed list: most name
// another permission family checked on a project (plugin:write is stored as
// the project action plugin_write).
const (
	ActionDeviceAgentSync Action = "device_agent_sync"
	ActionHooksIngest     Action = "hooks_ingest"
	ActionChannelsConnect Action = "channels_connect"
	ActionChannelsAdmin   Action = "channels_admin"
	ActionChannelsReadAll Action = "channels_read_all"
	ActionPluginWrite     Action = "plugin_write"
	ActionLogRead         Action = "log_read"
	ActionMemoryRead      Action = "memory_read"
	ActionMemoryWrite     Action = "memory_write"
)

// LegacyActions lists, per resource type, the compound actions Validate still
// accepts. Do not add to it: a new permission uses an action from Actions.
var LegacyActions = map[Kind][]Action{
	"org":     {ActionDeviceAgentSync, ActionHooksIngest},
	"project": {ActionChannelsConnect, ActionChannelsAdmin, ActionChannelsReadAll, ActionPluginWrite, ActionLogRead, ActionMemoryRead, ActionMemoryWrite},
}

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
