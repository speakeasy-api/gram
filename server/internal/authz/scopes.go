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
	ScopeMCPBlockedWrite         Scope = "mcp:blocked_write"
	ScopeMCPConnect              Scope = "mcp:connect"
	ScopeMCPBlockedConnect       Scope = "mcp:blocked_connect"
	ScopeEnvironmentRead         Scope = "environment:read"
	ScopeEnvironmentBlockedRead  Scope = "environment:blocked_read"
	ScopeEnvironmentWrite        Scope = "environment:write"
	ScopeEnvironmentBlockedWrite Scope = "environment:blocked_write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	ScopeChatRead                Scope = "chat:read"
)

type scopeVisibility int

const (
	scopeVisibilityUserVisible scopeVisibility = iota + 1
	scopeVisibilityInternal
)

const (
	ScopeVisibilityUserVisible = "user_visible"
	ScopeVisibilityInternal    = "internal"
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
	ScopeChatRead,
}

// scopeVisibilityByScope is the source of truth for whether a scope is exposed
// to clients as a first-class permission or only used internally for storage
// and evaluation.
var scopeVisibilityByScope = map[Scope]scopeVisibility{
	ScopeRoot:                    scopeVisibilityInternal,
	ScopeOrgRead:                 scopeVisibilityUserVisible,
	ScopeOrgBlockedRead:          scopeVisibilityInternal,
	ScopeOrgAdmin:                scopeVisibilityUserVisible,
	ScopeOrgBlockedAdmin:         scopeVisibilityInternal,
	ScopeProjectRead:             scopeVisibilityUserVisible,
	ScopeProjectBlockedRead:      scopeVisibilityInternal,
	ScopeProjectWrite:            scopeVisibilityUserVisible,
	ScopeProjectBlockedWrite:     scopeVisibilityInternal,
	ScopeMCPRead:                 scopeVisibilityUserVisible,
	ScopeMCPBlockedRead:          scopeVisibilityInternal,
	ScopeMCPWrite:                scopeVisibilityUserVisible,
	ScopeMCPBlockedWrite:         scopeVisibilityInternal,
	ScopeMCPConnect:              scopeVisibilityUserVisible,
	ScopeMCPBlockedConnect:       scopeVisibilityInternal,
	ScopeEnvironmentRead:         scopeVisibilityUserVisible,
	ScopeEnvironmentBlockedRead:  scopeVisibilityInternal,
	ScopeEnvironmentWrite:        scopeVisibilityUserVisible,
	ScopeEnvironmentBlockedWrite: scopeVisibilityInternal,
	ScopeRiskPolicyEvaluate:      scopeVisibilityUserVisible,
	ScopeRiskPolicyBypass:        scopeVisibilityUserVisible,
	ScopeChatRead:                scopeVisibilityUserVisible,
}

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

func ScopeVisibilityFor(scope Scope) (string, bool) {
	visibility, ok := scopeVisibilityByScope[scope]
	if !ok {
		return "", false
	}

	switch visibility {
	case scopeVisibilityUserVisible:
		return ScopeVisibilityUserVisible, true
	case scopeVisibilityInternal:
		return ScopeVisibilityInternal, true
	default:
		return "", false
	}
}

// scopeExpansions maps a checked scope to the other scopes that also satisfy
// it. For allow scopes, these are higher-privilege scopes. For blocklist
// scopes, these are broader blocklist scopes. Expansion is non-transitive: list
// every satisfying scope directly, since Check.expand only walks
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
	ScopeOrgBlockedAdmin:         {ScopeOrgBlockedRead},
	ScopeProjectRead:             {ScopeProjectWrite},
	ScopeProjectBlockedRead:      nil,
	ScopeProjectWrite:            nil,
	ScopeProjectBlockedWrite:     {ScopeProjectBlockedRead},
	ScopeMCPRead:                 {ScopeMCPWrite},
	ScopeMCPBlockedRead:          {ScopeMCPBlockedConnect},
	ScopeMCPWrite:                nil,
	ScopeMCPBlockedWrite:         {ScopeMCPBlockedRead, ScopeMCPBlockedConnect},
	ScopeMCPConnect:              {ScopeMCPRead, ScopeMCPWrite},
	ScopeMCPBlockedConnect:       nil,
	ScopeEnvironmentRead:         {ScopeEnvironmentWrite},
	ScopeEnvironmentBlockedRead:  nil,
	ScopeEnvironmentWrite:        nil,
	ScopeEnvironmentBlockedWrite: {ScopeEnvironmentBlockedRead},
	ScopeRiskPolicyEvaluate:      nil,
	ScopeRiskPolicyBypass:        nil,
	ScopeChatRead:                nil,
}

// scopeExclusions maps a checked base scope to the direct blocklist scope that
// stores exception grants for it. Broader blocklist scopes are handled by
// scopeExpansions on the blocklist scope itself.
var scopeExclusions = map[Scope]Scope{
	ScopeRoot:                    "",
	ScopeOrgRead:                 ScopeOrgBlockedRead,
	ScopeOrgBlockedRead:          "",
	ScopeOrgAdmin:                ScopeOrgBlockedAdmin,
	ScopeOrgBlockedAdmin:         "",
	ScopeProjectRead:             ScopeProjectBlockedRead,
	ScopeProjectBlockedRead:      "",
	ScopeProjectWrite:            ScopeProjectBlockedWrite,
	ScopeProjectBlockedWrite:     "",
	ScopeMCPRead:                 ScopeMCPBlockedRead,
	ScopeMCPBlockedRead:          "",
	ScopeMCPWrite:                ScopeMCPBlockedWrite,
	ScopeMCPBlockedWrite:         "",
	ScopeMCPConnect:              ScopeMCPBlockedConnect,
	ScopeMCPBlockedConnect:       "",
	ScopeEnvironmentRead:         ScopeEnvironmentBlockedRead,
	ScopeEnvironmentBlockedRead:  "",
	ScopeEnvironmentWrite:        ScopeEnvironmentBlockedWrite,
	ScopeEnvironmentBlockedWrite: "",
	ScopeRiskPolicyEvaluate:      ScopeRiskPolicyBypass,
	ScopeRiskPolicyBypass:        "",
	ScopeChatRead:                "",
}

// ExclusionScopeFor returns the scope that stores exception grants for the
// provided base scope.
func ExclusionScopeFor(scope Scope) (Scope, bool) {
	exclusion, ok := scopeExclusions[scope]
	return exclusion, ok && exclusion != ""
}

// scopeSubScopes is the user-visible inverse of scopeExpansions: for each
// higher-privilege scope, the lower scopes it implies (e.g.
// org:admin -> org:read). Internal blocklist expansions are intentionally not
// exposed as sub_scopes.
var scopeSubScopes map[Scope][]Scope

func init() {
	scopeSubScopes = make(map[Scope][]Scope)
	for lower, highers := range scopeExpansions {
		if scopeVisibilityByScope[lower] != scopeVisibilityUserVisible {
			continue
		}
		for _, h := range highers {
			if scopeVisibilityByScope[h] != scopeVisibilityUserVisible {
				continue
			}
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
