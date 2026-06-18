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
	ScopeRoot                    Scope = "root"
	ScopeOrgRead                 Scope = "org:read"
	ScopeOrgBlockedRead          Scope = "org:blocked_read"
	ScopeOrgAdmin                Scope = "org:admin"
	ScopeOrgBlockedAdmin         Scope = "org:blocked_admin"
	ScopeProjectRead             Scope = "project:read"
	ScopeProjectBlockedRead      Scope = "project:blocked_read"
	ScopeProjectWrite            Scope = "project:write"
	ScopeProjectBlockedWrite     Scope = "project:blocked_write"
	ScopeMCPRead                 Scope = "mcp:read"
	ScopeMCPBlockedRead          Scope = "mcp:blocked_read"
	ScopeMCPWrite                Scope = "mcp:write"
	ScopeMCPBlockedWrite         Scope = "mcp:blocked_write" //nolint:gosec // scope name, not a credential
	ScopeMCPConnect              Scope = "mcp:connect"
	ScopeMCPBlockedConnect       Scope = "mcp:blocked_connect"
	ScopeEnvironmentRead         Scope = "environment:read"
	ScopeEnvironmentBlockedRead  Scope = "environment:blocked_read"
	ScopeEnvironmentWrite        Scope = "environment:write"
	ScopeEnvironmentBlockedWrite Scope = "environment:blocked_write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
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

// NormalizeScope maps legacy exclusion scope names to their canonical
// blocklist-relation form. Keep this at the authz boundary so existing rows
// written with previous names keep evaluating and serializing correctly until
// the data migration lands.
func NormalizeScope(scope Scope) Scope {
	switch scope {
	case "org:read_exclusion", "org:read:deny":
		return ScopeOrgBlockedRead
	case "org:admin_exclusion", "org:admin:deny":
		return ScopeOrgBlockedAdmin
	case "project:read_exclusion", "project:read:deny":
		return ScopeProjectBlockedRead
	case "project:write_exclusion", "project:write:deny":
		return ScopeProjectBlockedWrite
	case "mcp:read_exclusion", "mcp:read:deny":
		return ScopeMCPBlockedRead
	case "mcp:write_exclusion", "mcp:write:deny":
		return ScopeMCPBlockedWrite
	case "mcp:block", "mcp:connect:deny":
		return ScopeMCPBlockedConnect
	case "environment:read_exclusion", "environment:read:deny":
		return ScopeEnvironmentBlockedRead
	case "environment:write_exclusion", "environment:write:deny":
		return ScopeEnvironmentBlockedWrite
	default:
		return scope
	}
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
	ScopeRoot:                    nil,
	ScopeOrgRead:                 {ScopeOrgAdmin},
	ScopeOrgBlockedRead:          nil,
	ScopeOrgAdmin:                nil,
	ScopeOrgBlockedAdmin:         nil,
	ScopeProjectRead:             {ScopeProjectWrite},
	ScopeProjectBlockedRead:      nil,
	ScopeProjectWrite:            nil,
	ScopeProjectBlockedWrite:     nil,
	ScopeMCPRead:                 {ScopeMCPWrite},
	ScopeMCPBlockedRead:          nil,
	ScopeMCPWrite:                nil,
	ScopeMCPBlockedWrite:         nil,
	ScopeMCPConnect:              {ScopeMCPRead, ScopeMCPWrite},
	ScopeMCPBlockedConnect:       nil,
	ScopeEnvironmentRead:         {ScopeEnvironmentWrite},
	ScopeEnvironmentBlockedRead:  nil,
	ScopeEnvironmentWrite:        nil,
	ScopeEnvironmentBlockedWrite: nil,
	ScopeRiskPolicyEvaluate:      nil,
	ScopeRiskPolicyBypass:        nil,
}

// scopeExclusions maps a checked scope to blocklist scopes that subtract it.
// Higher-privilege checks include lower-scope blocklist grants because higher
// scopes imply lower scopes through scopeExpansions. For example, mcp:write implies
// mcp:read, so mcp:blocked_read also subtracts an mcp:write check.
var scopeExclusions = map[Scope][]Scope{
	ScopeRoot:                    nil,
	ScopeOrgRead:                 {ScopeOrgBlockedRead},
	ScopeOrgBlockedRead:          nil,
	ScopeOrgAdmin:                {ScopeOrgBlockedAdmin, ScopeOrgBlockedRead},
	ScopeOrgBlockedAdmin:         nil,
	ScopeProjectRead:             {ScopeProjectBlockedRead},
	ScopeProjectBlockedRead:      nil,
	ScopeProjectWrite:            {ScopeProjectBlockedWrite, ScopeProjectBlockedRead},
	ScopeProjectBlockedWrite:     nil,
	ScopeMCPRead:                 {ScopeMCPBlockedRead, ScopeMCPBlockedConnect},
	ScopeMCPBlockedRead:          nil,
	ScopeMCPWrite:                {ScopeMCPBlockedWrite, ScopeMCPBlockedRead, ScopeMCPBlockedConnect},
	ScopeMCPBlockedWrite:         nil,
	ScopeMCPConnect:              {ScopeMCPBlockedConnect},
	ScopeMCPBlockedConnect:       nil,
	ScopeEnvironmentRead:         {ScopeEnvironmentBlockedRead},
	ScopeEnvironmentBlockedRead:  nil,
	ScopeEnvironmentWrite:        {ScopeEnvironmentBlockedWrite, ScopeEnvironmentBlockedRead},
	ScopeEnvironmentBlockedWrite: nil,
	ScopeRiskPolicyEvaluate:      {ScopeRiskPolicyBypass},
	ScopeRiskPolicyBypass:        nil,
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
