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
// Scopes with no higher-privilege implication (admin tiers) map to nil. Expansion is
// non-transitive: list every satisfying scope directly, since Check.expand only walks
// scopeExpansions[c.Scope] one step.
//
// environment:* scopes are checked at the project_id (no per-env granularity in the UI),
// so they share a resource kind and ID with project:* checks — this lets project-level
// grants cleanly satisfy environment-level checks via the standard selector matching.
var scopeExpansions = map[Scope][]Scope{
	ScopeRoot:             nil,
	ScopeOrgRead:          {ScopeOrgAdmin},
	ScopeOrgAdmin:         nil,
	ScopeProjectRead:      {ScopeProjectWrite},
	ScopeProjectWrite:     nil,
	ScopeMCPRead:          {ScopeMCPWrite},
	ScopeMCPWrite:         nil,
	ScopeMCPConnect:       {ScopeMCPRead, ScopeMCPWrite},
	ScopeEnvironmentRead:  {ScopeEnvironmentWrite, ScopeProjectRead, ScopeProjectWrite},
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
