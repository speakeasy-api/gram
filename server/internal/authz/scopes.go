package authz

import (
	"cmp"
	"slices"
	"strings"
)

// Scope identifies an authorization capability granted on a resource.
type Scope string

type ScopeParts struct {
	Resource string
	Action   string
}

const (
	ScopeRoot               Scope = "root"
	ScopeOrgRead            Scope = "org:read"
	ScopeOrgAdmin           Scope = "org:admin"
	ScopeProjectRead        Scope = "project:read"
	ScopeProjectWrite       Scope = "project:write"
	ScopeMCPRead            Scope = "mcp:read"
	ScopeMCPWrite           Scope = "mcp:write"
	ScopeMCPConnect         Scope = "mcp:connect"
	ScopeEnvironmentRead    Scope = "environment:read"
	ScopeEnvironmentWrite   Scope = "environment:write"
	ScopeRiskPolicyEvaluate Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass   Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
)

var adminScopes = []Scope{
	ScopeOrgRead,
	ScopeOrgAdmin,
	ScopeProjectRead,
	ScopeProjectWrite,
	ScopeMCPRead,
	ScopeMCPWrite,
	ScopeMCPConnect,
	ScopeEnvironmentRead,
	ScopeEnvironmentWrite,
}

var allScopes = append([]Scope{ScopeRiskPolicyBypass, ScopeRiskPolicyEvaluate}, adminScopes...)

var memberScopes = []Scope{
	ScopeOrgRead,
	ScopeProjectRead,
	ScopeMCPRead,
	ScopeMCPConnect,
	ScopeEnvironmentRead,
}

func (s Scope) Parts() ScopeParts {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok {
		return ScopeParts{Resource: string(s), Action: ""}
	}

	return ScopeParts{Resource: resource, Action: action}
}

// scopeExpansions maps a required scope to the higher-privilege scopes that also satisfy it.
// Scopes with no higher-privilege implication (admin tiers) map to nil. Expansion is
// non-transitive: list every satisfying scope directly, since Check.expand only walks
// scopeExpansions[c.Scope] one step.
//
// environment:* scopes are independent of project:* in the expansion graph (analogous to
// mcp:* scopes). Environment checks carry resource_kind="environment" with the project_id
// as a Dimensions constraint, so they don't share a resource kind with project checks and
// scope expansion across the boundary would never selector-match. Roles that need
// environment access must hold environment:read or environment:write explicitly — the
// system "admin" role does so via SystemRoleGrants.
//
// Preserves qstearns' non-escalation rule: project:read does not grant environment access
// (a generic project-viewer must not gain access to environment values, which include
// secrets).
var scopeExpansions = map[Scope][]Scope{
	ScopeRoot:               nil,
	ScopeOrgRead:            {ScopeOrgAdmin},
	ScopeOrgAdmin:           nil,
	ScopeProjectRead:        {ScopeProjectWrite},
	ScopeProjectWrite:       nil,
	ScopeMCPRead:            {ScopeMCPWrite},
	ScopeMCPWrite:           nil,
	ScopeMCPConnect:         {ScopeMCPRead, ScopeMCPWrite},
	ScopeEnvironmentRead:    {ScopeEnvironmentWrite},
	ScopeEnvironmentWrite:   nil,
	ScopeRiskPolicyEvaluate: nil,
	ScopeRiskPolicyBypass:   nil,
}

var internalScopes = map[Scope]bool{
	ScopeRoot:               false,
	ScopeOrgRead:            false,
	ScopeOrgAdmin:           false,
	ScopeProjectRead:        false,
	ScopeProjectWrite:       false,
	ScopeMCPRead:            false,
	ScopeMCPWrite:           false,
	ScopeMCPConnect:         false,
	ScopeEnvironmentRead:    false,
	ScopeEnvironmentWrite:   false,
	ScopeRiskPolicyEvaluate: false,
	ScopeRiskPolicyBypass:   false,
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

// IsInternalScope reports whether a scope is used as an internal relation
// rather than a user-facing RBAC capability.
func IsInternalScope(scope Scope) bool {
	return internalScopes[scope]
}
