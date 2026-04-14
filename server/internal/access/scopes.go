package access

import (
	"cmp"
	"slices"
)

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

// scopeSubScopes is the inverse of scopeExpansions: for each higher-privilege
// scope, the lower scopes it implies (e.g. org:admin → org:read).
var scopeSubScopes map[Scope][]Scope

func init() {
	scopeSubScopes = make(map[Scope][]Scope)
	for lower, highers := range scopeExpansions {
		for _, h := range highers {
			scopeSubScopes[h] = append(scopeSubScopes[h], lower)
		}
	}
	for _, lowers := range scopeSubScopes {
		slices.SortFunc(lowers, func(a, b Scope) int {
			return cmp.Compare(string(a), string(b))
		})
	}
}
