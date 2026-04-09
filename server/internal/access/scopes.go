package access

// Scope identifies an access capability granted on a resource.
type Scope string

const (
	ScopeRoot       Scope = "root"
	ScopeOrgRead    Scope = "org:read"
	ScopeOrgAdmin   Scope = "org:admin"
	ScopeBuildRead  Scope = "build:read"
	ScopeBuildWrite Scope = "build:write"
	ScopeMCPRead    Scope = "mcp:read"
	ScopeMCPWrite   Scope = "mcp:write"
	ScopeMCPConnect Scope = "mcp:connect"
)

// scopeExpansions maps a required scope to the higher-privilege scopes that also satisfy it.
// Scopes with no higher-privilege implication (admin tiers and mcp:connect) map to nil.
// mcp:connect is a distinct capability — it is not implied by mcp:write.
var scopeExpansions = map[Scope][]Scope{
	ScopeRoot:       nil,
	ScopeOrgRead:    {ScopeOrgAdmin},
	ScopeOrgAdmin:   nil,
	ScopeBuildRead:  {ScopeBuildWrite},
	ScopeBuildWrite: nil,
	ScopeMCPRead:    {ScopeMCPWrite},
	ScopeMCPWrite:   nil,
	ScopeMCPConnect: nil,
}
