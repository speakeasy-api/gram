package access

// Scope identifies an access capability granted on a resource.
type Scope string

const (
	ScopeOrgRead    Scope = "org:read"
	ScopeOrgAdmin   Scope = "org:admin"
	ScopeBuildRead  Scope = "build:read"
	ScopeBuildWrite Scope = "build:write"
	ScopeMCPRead    Scope = "mcp:read"
	ScopeMCPWrite   Scope = "mcp:write"
	ScopeMCPConnect Scope = "mcp:connect"
)
