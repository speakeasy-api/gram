// The vocabulary of the authorization model: the names grants and checks are
// written in. This file is the one place they are declared. authzgen copies
// everything below the package line into package authz as vocabulary_gen.go,
// so the model and the evaluator share these names without importing each
// other. Keep it free of imports and of anything only the registry needs.
package registry

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

// GrantSource says where the grants of a scope are set: through roles, or
// through risk policies. Reads can show every grant, but a write must come
// from the scope's source, so one part of the product cannot replace grants
// that another part sets. The stored values predate the name: "access" means
// roles.
type GrantSource string

const (
	GrantSourceRoles      GrantSource = "access"
	GrantSourceRiskPolicy GrantSource = "risk_policy"
)

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchLoose skips grant keys the check does not carry. It is today's
	// default for every resource type except auth clients.
	MatchLoose MatchMode = iota
	// MatchStrict requires every grant key to be present in the check.
	MatchStrict
)

// The names of the built-in roles, and the value that stands for every
// resource in a selector.
const (
	SystemRoleAdmin  = "admin"
	SystemRoleMember = "member"
	WildcardResource = "*"
)

// Selector keys. resource_kind and resource_id are on every grant; the rest
// are dimensions a resource type or a permission declares.
const (
	SelectorKeyResourceKind   = "resource_kind"
	SelectorKeyResourceID     = "resource_id"
	SelectorKeyTool           = "tool"
	SelectorKeyDisposition    = "disposition"
	SelectorKeyProjectID      = "project_id"
	SelectorKeyServerURL      = "server_url"
	SelectorKeyServerIdentity = "server_identity"
	// SelectorKeyClientType and SelectorKeyConnectionOwner narrow mcp:auth and
	// mcp:blocked_auth grants to an auth client type and to the owner of a
	// personal connection.
	SelectorKeyClientType      = "client_type"
	SelectorKeyConnectionOwner = "connection_owner"
	// SelectorKeyUpstreamClient narrows mcp:auth and mcp:blocked_auth grants
	// on personal clients to one upstream OAuth client, the
	// remote_session_clients id behind it. With a connection owner it names one
	// logical connection across every auth client sharing that upstream
	// client, which is what a connection owner's block names.
	SelectorKeyUpstreamClient = "upstream_client"
)

// Auth client types an mcp:auth check can name. Only remote_session clients
// are personal: their checks carry the connection owner.
const (
	AuthClientTypeRemoteSession    = "remote_session"
	AuthClientTypeServiceAccount   = "service_account"
	AuthClientTypeStaticCredential = "static_credential" //nolint:gosec // client type name, not a credential
	AuthClientTypePassThrough      = "pass_through"
	AuthClientTypeEMA              = "ema"
)

// Disposition values matching MCP tool annotation hint names (snake_case, no _hint suffix).
const (
	DispositionReadOnly    = "read_only"
	DispositionDestructive = "destructive"
	DispositionIdempotent  = "idempotent"
	DispositionOpenWorld   = "open_world"
)
