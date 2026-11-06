package zedspike

// System roles seeded into every organization.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// DefaultGrant is one grant a new organization is seeded with.
type DefaultGrant struct {
	Scope    Scope
	Selector map[string]string
}

// DefaultRoles is what each system role is granted when an organization is
// created. It is seed data, not schema: rbac.zed says what a grant means,
// this table says who starts with which grant. System roles stay editable,
// so changing a line here reaches new organizations only; every change must
// ship with a decision about existing ones (a backfill migration, or a note
// that none is wanted).
var DefaultRoles = map[string][]Scope{
	RoleAdmin: {
		ScopeOrgRead, ScopeOrgAdmin,
		ScopeProjectRead, ScopeProjectWrite, ScopePluginWrite,
		ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect, ScopeMCPAuth,
		ScopeEnvironmentRead, ScopeEnvironmentWrite,
		ScopeSkillRead, ScopeSkillWrite,
		ScopeAssistantRead, ScopeAssistantWrite,
		// types the schema does not declare yet
		"agent:read", "agent:write", "agent:authorize", "agent:transfer",
		"workload:read", "workload:write",
	},
	RoleMember: {
		ScopeOrgRead,
		ScopeProjectRead,
		ScopeMCPRead, ScopeMCPConnect, ScopeMCPAuth,
		ScopeSkillRead,
		ScopeAssistantRead,
	},
	// Never a default for any role: chat:read and chat:write (other members'
	// transcripts), log:read (every member's tool arguments and results),
	// environment:read for members (environment values hold secrets).
}

// DefaultForEveryone is granted to user:all in every organization: each
// person may use their own personal connections.
var DefaultForEveryone = []DefaultGrant{
	{Scope: ScopeMCPAuth, Selector: map[string]string{
		"resource_kind": "mcp_auth_client", "resource_id": "*",
		SelectorClientType: "remote_session", SelectorConnectionOwner: "$represented_user",
	}},
}
