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
	ScopeRoot                      Scope = "root"
	ScopeOrgRead                   Scope = "org:read"
	ScopeOrgReadExclusion          Scope = "org:read_exclusion"
	ScopeOrgAdmin                  Scope = "org:admin"
	ScopeOrgAdminExclusion         Scope = "org:admin_exclusion"
	ScopeProjectRead               Scope = "project:read"
	ScopeProjectReadExclusion      Scope = "project:read_exclusion"
	ScopeProjectWrite              Scope = "project:write"
	ScopeProjectWriteExclusion     Scope = "project:write_exclusion"
	ScopeMCPRead                   Scope = "mcp:read"
	ScopeMCPReadExclusion          Scope = "mcp:read_exclusion"
	ScopeMCPWrite                  Scope = "mcp:write"
	ScopeMCPWriteExclusion         Scope = "mcp:write_exclusion" //nolint:gosec // scope name, not a credential
	ScopeMCPConnect                Scope = "mcp:connect"
	ScopeMCPBlock                  Scope = "mcp:block"
	ScopeEnvironmentRead           Scope = "environment:read"
	ScopeEnvironmentReadExclusion  Scope = "environment:read_exclusion"
	ScopeEnvironmentWrite          Scope = "environment:write"
	ScopeEnvironmentWriteExclusion Scope = "environment:write_exclusion"
	ScopeRiskPolicyEvaluate        Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass          Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
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
	ScopeRoot:                      nil,
	ScopeOrgRead:                   {ScopeOrgAdmin},
	ScopeOrgReadExclusion:          nil,
	ScopeOrgAdmin:                  nil,
	ScopeOrgAdminExclusion:         nil,
	ScopeProjectRead:               {ScopeProjectWrite},
	ScopeProjectReadExclusion:      nil,
	ScopeProjectWrite:              nil,
	ScopeProjectWriteExclusion:     nil,
	ScopeMCPRead:                   {ScopeMCPWrite},
	ScopeMCPReadExclusion:          nil,
	ScopeMCPWrite:                  nil,
	ScopeMCPWriteExclusion:         nil,
	ScopeMCPConnect:                {ScopeMCPRead, ScopeMCPWrite},
	ScopeMCPBlock:                  nil,
	ScopeEnvironmentRead:           {ScopeEnvironmentWrite},
	ScopeEnvironmentReadExclusion:  nil,
	ScopeEnvironmentWrite:          nil,
	ScopeEnvironmentWriteExclusion: nil,
	ScopeRiskPolicyEvaluate:        nil,
	ScopeRiskPolicyBypass:          nil,
}

// scopeExclusions maps a checked scope to the scopes that subtract it.
// Higher-privilege checks include lower-scope exclusions because higher scopes
// imply lower scopes through scopeExpansions. For example, mcp:write implies
// mcp:read, so mcp:read_exclusion also subtracts mcp:write.
var scopeExclusions = map[Scope][]Scope{
	ScopeRoot:                      nil,
	ScopeOrgRead:                   {ScopeOrgReadExclusion},
	ScopeOrgReadExclusion:          nil,
	ScopeOrgAdmin:                  {ScopeOrgAdminExclusion, ScopeOrgReadExclusion},
	ScopeOrgAdminExclusion:         nil,
	ScopeProjectRead:               {ScopeProjectReadExclusion},
	ScopeProjectReadExclusion:      nil,
	ScopeProjectWrite:              {ScopeProjectWriteExclusion, ScopeProjectReadExclusion},
	ScopeProjectWriteExclusion:     nil,
	ScopeMCPRead:                   {ScopeMCPReadExclusion, ScopeMCPBlock},
	ScopeMCPReadExclusion:          nil,
	ScopeMCPWrite:                  {ScopeMCPWriteExclusion, ScopeMCPReadExclusion, ScopeMCPBlock},
	ScopeMCPWriteExclusion:         nil,
	ScopeMCPConnect:                {ScopeMCPBlock},
	ScopeMCPBlock:                  nil,
	ScopeEnvironmentRead:           {ScopeEnvironmentReadExclusion},
	ScopeEnvironmentReadExclusion:  nil,
	ScopeEnvironmentWrite:          {ScopeEnvironmentWriteExclusion, ScopeEnvironmentReadExclusion},
	ScopeEnvironmentWriteExclusion: nil,
	ScopeRiskPolicyEvaluate:        {ScopeRiskPolicyBypass},
	ScopeRiskPolicyBypass:          nil,
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
