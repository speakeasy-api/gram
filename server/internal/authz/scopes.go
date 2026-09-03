package authz

import (
	"cmp"
	"fmt"
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
	ScopeSkillRead               Scope = "skill:read"
	ScopeSkillBlockedRead        Scope = "skill:blocked_read"
	ScopeSkillWrite              Scope = "skill:write"
	ScopeSkillBlockedWrite       Scope = "skill:blocked_write"
	ScopeRiskPolicyEvaluate      Scope = "risk_policy:evaluate"
	ScopeRiskPolicyBypass        Scope = "risk_policy:bypass" //nolint:gosec // scope name, not a credential
	ScopeRiskPolicyBlock         Scope = "risk_policy:block"
	ScopeChatRead                Scope = "chat:read"
	ScopeChatWrite               Scope = "chat:write"
	ScopeAgentRead               Scope = "agent:read"
	ScopeAgentWrite              Scope = "agent:write"
	ScopeAgentAuthorize          Scope = "agent:authorize"
	ScopeAgentTransfer           Scope = "agent:transfer"
)

// Retired scope names remain registered as tombstones so stored delegated
// policy can still be parsed and displayed. Retired names must never be reused.
const (
	scopeMCPApprovalReadTombstone   Scope = "mcp_approval:read"
	scopeMCPApprovalDecideTombstone Scope = "mcp_approval:decide"
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
	ScopeSkillRead,
	ScopeSkillWrite,
	ScopeAgentRead,
	ScopeAgentWrite,
	ScopeAgentAuthorize,
	ScopeAgentTransfer,
	// chat:read and chat:write are intentionally NOT defaults for any system
	// role: reading other members' session transcripts is sensitive, and
	// mutating them (rename, feedback, delete) is destructive, so both must be
	// granted explicitly via a custom role grant. Pinning follows chat:read —
	// it is a shared project bookmark, not a transcript mutation. Everyone
	// reads and mutates their own sessions via owner-matching in the chat
	// handlers regardless. chat:write satisfies chat:read via scopeExpansions,
	// so a session reviewer who should not be able to delete gets chat:read
	// alone.
}

// AgentRuntimeScopeRegistryVersion identifies an immutable version of the
// application-owned allowlist used by agent direct and delegated policy.
type AgentRuntimeScopeRegistryVersion int

const (
	AgentRuntimeScopeRegistryVersion1 AgentRuntimeScopeRegistryVersion = 1

	CurrentAgentRuntimeScopeRegistryVersion = AgentRuntimeScopeRegistryVersion1
)

// ScopeLifecycle distinguishes active scope registrations from retained
// retirement tombstones.
type ScopeLifecycle string

const (
	ScopeLifecycleActive  ScopeLifecycle = "active"
	ScopeLifecycleRetired ScopeLifecycle = "retired"
)

type scopeDefinition struct {
	visibility            scopeVisibility
	lifecycle             ScopeLifecycle
	agentRuntimeSafeSince AgentRuntimeScopeRegistryVersion
}

func activeScope(visibility scopeVisibility) scopeDefinition {
	return scopeDefinition{visibility: visibility, lifecycle: ScopeLifecycleActive}
}

func agentSafeScope(visibility scopeVisibility) scopeDefinition {
	return scopeDefinition{
		visibility:            visibility,
		lifecycle:             ScopeLifecycleActive,
		agentRuntimeSafeSince: AgentRuntimeScopeRegistryVersion1,
	}
}

// scopeDefinitions is the application-owned scope registry. Every active scope
// and every retained retirement tombstone must have an entry.
var scopeDefinitions = map[Scope]scopeDefinition{
	ScopeRoot:                       activeScope(scopeVisibilityInternal),
	ScopeOrgRead:                    activeScope(scopeVisibilityUserVisible),
	ScopeOrgBlockedRead:             activeScope(scopeVisibilityInternal),
	ScopeOrgAdmin:                   activeScope(scopeVisibilityUserVisible),
	ScopeOrgBlockedAdmin:            activeScope(scopeVisibilityInternal),
	ScopeProjectRead:                agentSafeScope(scopeVisibilityUserVisible),
	ScopeProjectBlockedRead:         activeScope(scopeVisibilityInternal),
	ScopeProjectWrite:               agentSafeScope(scopeVisibilityUserVisible),
	ScopeProjectBlockedWrite:        activeScope(scopeVisibilityInternal),
	ScopeMCPRead:                    agentSafeScope(scopeVisibilityUserVisible),
	ScopeMCPBlockedRead:             activeScope(scopeVisibilityInternal),
	ScopeMCPWrite:                   agentSafeScope(scopeVisibilityUserVisible),
	ScopeMCPBlockedWrite:            activeScope(scopeVisibilityInternal),
	ScopeMCPConnect:                 agentSafeScope(scopeVisibilityUserVisible),
	ScopeMCPBlockedConnect:          activeScope(scopeVisibilityInternal),
	ScopeEnvironmentRead:            agentSafeScope(scopeVisibilityUserVisible),
	ScopeEnvironmentBlockedRead:     activeScope(scopeVisibilityInternal),
	ScopeEnvironmentWrite:           agentSafeScope(scopeVisibilityUserVisible),
	ScopeEnvironmentBlockedWrite:    activeScope(scopeVisibilityInternal),
	ScopeSkillRead:                  agentSafeScope(scopeVisibilityUserVisible),
	ScopeSkillBlockedRead:           activeScope(scopeVisibilityInternal),
	ScopeSkillWrite:                 agentSafeScope(scopeVisibilityUserVisible),
	ScopeSkillBlockedWrite:          activeScope(scopeVisibilityInternal),
	ScopeRiskPolicyEvaluate:         agentSafeScope(scopeVisibilityUserVisible),
	ScopeRiskPolicyBypass:           activeScope(scopeVisibilityUserVisible),
	ScopeRiskPolicyBlock:            activeScope(scopeVisibilityUserVisible),
	ScopeChatRead:                   activeScope(scopeVisibilityUserVisible),
	ScopeChatWrite:                  activeScope(scopeVisibilityUserVisible),
	ScopeAgentRead:                  activeScope(scopeVisibilityUserVisible),
	ScopeAgentWrite:                 activeScope(scopeVisibilityUserVisible),
	ScopeAgentAuthorize:             activeScope(scopeVisibilityUserVisible),
	ScopeAgentTransfer:              activeScope(scopeVisibilityUserVisible),
	scopeMCPApprovalReadTombstone:   {lifecycle: ScopeLifecycleRetired},
	scopeMCPApprovalDecideTombstone: {lifecycle: ScopeLifecycleRetired},
}

var memberScopes = []Scope{
	ScopeOrgRead,
	ScopeProjectRead,
	ScopeMCPRead,
	ScopeMCPConnect,
	ScopeSkillRead,
	// environment:read is intentionally NOT a default for members: environment
	// values include secrets, so viewing them must be granted explicitly via a
	// custom role. Admins retain environment:read/write via adminScopes.
	//
	// Most Observe pages are separately gated on org:admin. The Identities
	// roster and required-employee-scoped Shadow AI read are project:read
	// surfaces; identity detail resolution separately requires org:read.
}

func (s Scope) Parts() ScopeParts {
	resource, action, ok := strings.Cut(string(s), ":")
	if !ok {
		return ScopeParts{Resource: string(s), Action: ""}
	}

	return ScopeParts{Resource: resource, Action: action}
}

func ScopeVisibilityFor(scope Scope) (string, bool) {
	definition, ok := scopeDefinitions[scope]
	if !ok || definition.lifecycle != ScopeLifecycleActive {
		return "", false
	}

	switch definition.visibility {
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
	ScopeSkillRead:               {ScopeSkillWrite},
	ScopeSkillBlockedRead:        nil,
	ScopeSkillWrite:              nil,
	ScopeSkillBlockedWrite:       {ScopeSkillBlockedRead},
	ScopeRiskPolicyEvaluate:      nil,
	ScopeRiskPolicyBypass:        nil,
	ScopeRiskPolicyBlock:         nil,
	ScopeChatRead:                {ScopeChatWrite},
	ScopeChatWrite:               nil,
	ScopeAgentRead:               nil,
	ScopeAgentWrite:              nil,
	ScopeAgentAuthorize:          nil,
	ScopeAgentTransfer:           nil,
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
	ScopeSkillRead:               ScopeSkillBlockedRead,
	ScopeSkillBlockedRead:        "",
	ScopeSkillWrite:              ScopeSkillBlockedWrite,
	ScopeSkillBlockedWrite:       "",
	ScopeRiskPolicyEvaluate:      ScopeRiskPolicyBypass,
	ScopeRiskPolicyBypass:        "",
	ScopeRiskPolicyBlock:         "",
	ScopeChatRead:                "",
	ScopeChatWrite:               "",
	ScopeAgentRead:               "",
	ScopeAgentWrite:              "",
	ScopeAgentAuthorize:          "",
	ScopeAgentTransfer:           "",
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
		if scopeDefinitions[lower].visibility != scopeVisibilityUserVisible {
			continue
		}
		for _, h := range highers {
			if scopeDefinitions[h].visibility != scopeVisibilityUserVisible {
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

// ScopeLifecycleFor returns the lifecycle of a known active scope or retained
// tombstone. Unknown scopes return false.
func ScopeLifecycleFor(scope Scope) (ScopeLifecycle, bool) {
	definition, ok := scopeDefinitions[scope]
	if !ok {
		return "", false
	}
	return definition.lifecycle, true
}

// AgentRuntimeScopeImplicationClosure returns the complete set of checks that a
// grant for scope can satisfy, including scope itself.
func AgentRuntimeScopeImplicationClosure(scope Scope) []Scope {
	seen := map[Scope]struct{}{scope: {}}
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

// ValidateAgentRuntimeScope verifies that a scope and its complete implication
// closure are active and explicitly safe in the requested registry version.
func ValidateAgentRuntimeScope(version AgentRuntimeScopeRegistryVersion, scope Scope) error {
	if version < AgentRuntimeScopeRegistryVersion1 || version > CurrentAgentRuntimeScopeRegistryVersion {
		return fmt.Errorf("unsupported agent runtime scope registry version %d", version)
	}

	for _, implied := range AgentRuntimeScopeImplicationClosure(scope) {
		definition, ok := scopeDefinitions[implied]
		if !ok {
			return fmt.Errorf("scope %q is not registered", implied)
		}
		if definition.lifecycle != ScopeLifecycleActive {
			return fmt.Errorf("scope %q is retired", implied)
		}
		if definition.agentRuntimeSafeSince == 0 || definition.agentRuntimeSafeSince > version {
			return fmt.Errorf("scope %q is not agent-runtime-safe", implied)
		}
	}

	return nil
}

// IsAgentRuntimeScopeSafe reports whether the scope is valid for direct agent
// policy or delegated credential policy in the requested registry version.
func IsAgentRuntimeScopeSafe(version AgentRuntimeScopeRegistryVersion, scope Scope) bool {
	return ValidateAgentRuntimeScope(version, scope) == nil
}
