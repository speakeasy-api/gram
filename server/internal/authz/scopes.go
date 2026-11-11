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

var adminScopes = []Scope{
	ScopeOrgRead,
	ScopeOrgAdmin,
	ScopeProjectRead,
	ScopeProjectWrite,
	ScopeMCPRead,
	ScopeMCPWrite,
	ScopeMCPConnect,
	ScopeMCPAuth,
	ScopeEnvironmentRead,
	ScopeEnvironmentWrite,
	ScopeSkillRead,
	ScopeSkillWrite,
	ScopeAssistantRead,
	ScopeAssistantWrite,
	ScopePluginWrite,
	ScopeAgentRead,
	ScopeAgentWrite,
	ScopeAgentAuthorize,
	ScopeAgentTransfer,
	// workload:read is a member default (see memberScopes below) so that team
	// members can view the Access Hub. workload:write remains admin-only:
	// assigning an agent to a workload is the same delegation as issuing that
	// agent a credential.
	ScopeWorkloadRead,
	ScopeWorkloadWrite,
	// chat:read and chat:write are intentionally NOT defaults for any system
	// role: reading other members' session transcripts is sensitive, and
	// mutating them (rename, feedback, delete) is destructive, so both must be
	// granted explicitly via a custom role grant. Pinning follows chat:read —
	// it is a shared project bookmark, not a transcript mutation. Everyone
	// reads and mutates their own sessions via owner-matching in the chat
	// handlers regardless. chat:write satisfies chat:read via scopeExpansions,
	// so a session reviewer who should not be able to delete gets chat:read
	// alone.
	//
	// log:read is likewise NOT a default for any system role: raw tool-call
	// logs carry tool arguments, results, prompt text, and hook payloads from
	// every member of a project, so viewing them must be granted explicitly per
	// project. Neither project:* nor org:admin implies it.
}

var memberScopes = []Scope{
	ScopeOrgRead,
	ScopeProjectRead,
	ScopeMCPRead,
	ScopeMCPConnect,
	ScopeMCPAuth,
	ScopeSkillRead,
	ScopeAssistantRead,
	ScopeWorkloadRead,
	// environment:read is intentionally NOT a default for members: environment
	// values include secrets, so viewing them must be granted explicitly via a
	// custom role. Admins retain environment:read/write via adminScopes.
	//
	// Most Observe pages are separately gated on org:admin. The Identities
	// roster and required-employee-scoped Shadow AI read are project:read
	// surfaces; identity detail resolution separately requires org:read.
	//
	// workload:read is a member default so that team members can view the
	// Access Hub in preview deployments and development environments.
	// workload:write remains admin-only since it allows granting machine access.
}

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
