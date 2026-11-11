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
	ScopeMCPAuth                 Scope = "mcp:auth"
	ScopeMCPBlockedAuth          Scope = "mcp:blocked_auth"
	ScopeEnvironmentRead         Scope = "environment:read"
	ScopeEnvironmentBlockedRead  Scope = "environment:blocked_read"
	ScopeEnvironmentWrite        Scope = "environment:write"
	ScopeEnvironmentBlockedWrite Scope = "environment:blocked_write"
	ScopeSkillRead               Scope = "skill:read"
	ScopeSkillBlockedRead        Scope = "skill:blocked_read"
	ScopeSkillWrite              Scope = "skill:write"
	ScopeSkillBlockedWrite       Scope = "skill:blocked_write"
	ScopePluginWrite             Scope = "plugin:write"
	ScopePluginBlockedWrite      Scope = "plugin:blocked_write"
	ScopeAssistantRead           Scope = "assistant:read"
	ScopeAssistantBlockedRead    Scope = "assistant:blocked_read"
	ScopeAssistantWrite          Scope = "assistant:write"
	ScopeAssistantBlockedWrite   Scope = "assistant:blocked_write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	ScopeRiskPolicyBlock         Scope = "risk_policy:block"
	ScopeChatRead                Scope = "chat:read"
	ScopeChatWrite               Scope = "chat:write"
	ScopeInferenceExecute        Scope = "inference:execute"
	ScopeLogRead                 Scope = "log:read"
	ScopeAgentRead               Scope = "agent:read"
	ScopeAgentWrite              Scope = "agent:write"
	ScopeAgentAuthorize          Scope = "agent:authorize"
	ScopeAgentTransfer           Scope = "agent:transfer"

	// Managing an organization's workload identity trust policy: its issuers, its
	// admitted subjects, and which agent each inherits its policy from. Presence in
	// the admitted set is itself the grant of machine access, so it gets a
	// permission that says so rather than riding a project or issuer scope whose
	// product meaning is something else.
	//
	// "workload" here names the resource being configured, not
	// urn.PrincipalTypeWorkload, which is also "workload" and names the machine
	// principal itself. See ResourceKindWorkload.
	ScopeWorkloadRead         Scope = "workload:read"
	ScopeWorkloadBlockedRead  Scope = "workload:blocked_read"
	ScopeWorkloadWrite        Scope = "workload:write"
	ScopeWorkloadBlockedWrite Scope = "workload:blocked_write"
	// Device-agent sync and hook ingestion for agent-principal keys. Human
	// callers reach these routes through transport scopes, not these grants.
	ScopeOrgDeviceAgentSync Scope = "org:device_agent_sync"
	ScopeOrgHooksIngest     Scope = "org:hooks_ingest"
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

// scopeVisibilityByScope says whether a scope is exposed to clients as a
// first-class permission or only used internally for storage and evaluation.
// Derived from Model: declared permissions are user-visible, generated blocks
// and root are internal.
var scopeVisibilityByScope = Model.deriveScopeVisibility()

func (s Scope) Parts() ScopeParts {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok {
		return ScopeParts{Resource: string(s), Action: ""}
	}

	return ScopeParts{Resource: resource, Action: action}
}

// RegisteredScopes returns the scopes known to authorization in stable order.
func RegisteredScopes() []Scope {
	scopes := make([]Scope, 0, len(scopeVisibilityByScope))
	for scope := range scopeVisibilityByScope {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	return scopes
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
// scopes, these are broader blocklist scopes. Expansion is non-transitive: the
// derivation lists every satisfying scope directly, since Check.expand only
// walks scopeExpansions[c.Scope] one step.
//
// Derived from Model (Implies and Exclusions); see registry_resources.go for why
// each family relates the way it does.
var scopeExpansions = Model.deriveScopeExpansions()

// scopeExclusions maps a checked base scope to the direct blocklist scope that
// stores exception grants for it. Broader blocklist scopes are handled by
// scopeExpansions on the blocklist scope itself. Derived from Model.
var scopeExclusions = Model.deriveScopeExclusions()

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

// ScopeImplicationClosure returns the complete set of checks that a grant for
// scope can satisfy, including scope itself.
func ScopeImplicationClosure(scope Scope) []Scope {
	seen := make(map[Scope]struct{}, 1)
	seen[scope] = struct{}{}
	queue := []Scope{scope}
	for len(queue) > 0 {
		granted := queue[0]
		queue = queue[1:]
		for checked, satisfying := range scopeExpansions {
			if !slices.Contains(satisfying, granted) {
				continue
			}
			if _, ok := seen[checked]; ok {
				continue
			}
			seen[checked] = struct{}{}
			queue = append(queue, checked)
		}
	}

	closure := make([]Scope, 0, len(seen))
	for implied := range seen {
		closure = append(closure, implied)
	}
	slices.Sort(closure)
	return closure
}
