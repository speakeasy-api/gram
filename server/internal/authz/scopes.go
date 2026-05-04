package authz

import (
	"cmp"
	"slices"
)

// Scope identifies an authorization capability granted on a resource.
type Scope string

const (
	ScopeRoot             Scope = "root"
	ScopeOrgRead          Scope = "org:read"
	ScopeOrgAdmin         Scope = "org:admin"
	ScopeProjectRead      Scope = "project:read"
	ScopeProjectWrite     Scope = "project:write"
	ScopeMCPRead          Scope = "mcp:read"
	ScopeMCPWrite         Scope = "mcp:write"
	ScopeMCPConnect       Scope = "mcp:connect"
	ScopeEnvironmentRead  Scope = "environment:read"
	ScopeEnvironmentWrite Scope = "environment:write"
)

// scopeExpansions maps a required scope to the higher-privilege scopes that also satisfy it.
// Scopes with no higher-privilege implication (admin tiers) map to nil.
//
// environment:write expands to project:write because both target the project as the
// resource (env:write at projectID is satisfied by project:write at projectID — same kind,
// same ID — so the cross-scope check resolves correctly via selector matching).
//
// environment:read does NOT expand to project:read/project:write because the resource IDs
// differ across kinds: env:read is checked at the environment's UUID, but expansion would
// produce a project:read variant still bound to that env UUID, which can never match a
// fine-grained project:read grant pinned to the project UUID. Callers that need to honor
// project-level read grants for environment reads must use Engine.RequireAny with explicit
// {env:read at envID} and {project:read at projectID} alternatives.
var scopeExpansions = map[Scope][]Scope{
	ScopeRoot:             nil,
	ScopeOrgRead:          {ScopeOrgAdmin},
	ScopeOrgAdmin:         nil,
	ScopeProjectRead:      {ScopeProjectWrite},
	ScopeProjectWrite:     nil,
	ScopeMCPRead:          {ScopeMCPWrite},
	ScopeMCPWrite:         nil,
	ScopeMCPConnect:       {ScopeMCPRead, ScopeMCPWrite},
	ScopeEnvironmentRead:  {ScopeEnvironmentWrite},
	ScopeEnvironmentWrite: {ScopeProjectWrite},
}

// scopeSubScopes is the inverse of scopeExpansions: for each higher-privilege
// scope, the lower scopes it implies (e.g. org:admin -> org:read).
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

func CalculateSubScopes(scope Scope) []string {
	lowers := scopeSubScopes[scope]
	out := make([]string, len(lowers))
	for i, s := range lowers {
		out[i] = string(s)
	}
	return out
}
