package authz

import (
	"fmt"
	"maps"
	"slices"
)

// ScopeDef is the single declaration of an allow scope. Everything
// authorization needs to know about the scope is a field here, so adding or
// changing a scope means editing one entry instead of a set of parallel
// tables.
//
// The registry is being adopted slice by slice. A scope declared here must not
// also appear in the legacy tables (scopeVisibilityByScope, scopeExpansions,
// scopeExclusions, adminScopes, memberScopes, scopeGrantSurfaces); package
// initialisation panics if it does. Fields are added only when a migrated
// slice needs them.
type ScopeDef struct {
	// Scope is the allow scope's name, "<resource>:<action>".
	Scope Scope
	// ResourceKind is the selector resource_kind grants and checks for this
	// scope carry.
	ResourceKind string
	// Implies lists the scopes a grant of Scope also satisfies. Implication is
	// one step, not transitive: list every implied scope directly.
	Implies []Scope
	// SystemRoles lists the built-in roles that hold Scope by default.
	SystemRoles []string
	// AgentRuntimeSafeSince is the first agent runtime scope registry version
	// in which agent direct and delegated policy may use Scope. Zero means
	// never.
	AgentRuntimeSafeSince int
	// Block, when set, gives Scope a "<resource>:blocked_<action>" twin that
	// stores exception grants subtracting from it. The twin is internal, shares
	// Scope's resource kind and grant surface, and mirrors Implies in reverse:
	// if A implies B, blocking B also blocks A.
	Block *BlockDef
}

// BlockDef declares the blocklist twin of a ScopeDef.
type BlockDef struct {
	// AgentRuntimeSafeSince is the twin's own agent runtime safety version,
	// which can differ from the allow scope's.
	AgentRuntimeSafeSince int
}

// scopeRegistry declares the scopes that have moved out of the legacy tables.
var scopeRegistry = []ScopeDef{
	{
		Scope:                 ScopeOrgRead,
		ResourceKind:          ResourceKindOrg,
		Implies:               nil,
		SystemRoles:           []string{SystemRoleAdmin, SystemRoleMember},
		AgentRuntimeSafeSince: 0,
		Block:                 &BlockDef{AgentRuntimeSafeSince: 0},
	},
	{
		Scope:                 ScopeOrgAdmin,
		ResourceKind:          ResourceKindOrg,
		Implies:               []Scope{ScopeOrgRead},
		SystemRoles:           []string{SystemRoleAdmin},
		AgentRuntimeSafeSince: 0,
		Block:                 &BlockDef{AgentRuntimeSafeSince: 0},
	},
}

// registeredScope is one resolved registry entry: an allow scope or a derived
// blocklist twin.
type registeredScope struct {
	scope                 Scope
	resourceKind          string
	visibility            scopeVisibility
	surface               GrantSurface
	satisfiedBy           []Scope
	exclusion             Scope
	systemRoles           []string
	agentRuntimeSafeSince int
}

// registeredScopes indexes the resolved registry by scope name.
var registeredScopes = resolveScopeRegistry(scopeRegistry)

// BlockedScope returns the blocklist twin name for an allow scope.
func BlockedScope(scope Scope) Scope {
	parts := scope.Parts()
	return Scope(parts.Resource + ":blocked_" + parts.Action)
}

func resolveScopeRegistry(defs []ScopeDef) map[Scope]*registeredScope {
	out := make(map[Scope]*registeredScope, len(defs)*2)
	add := func(entry *registeredScope) {
		if _, dup := out[entry.scope]; dup {
			panic(fmt.Sprintf("authz: scope %q declared twice in the scope registry", entry.scope))
		}
		out[entry.scope] = entry
	}

	for _, def := range defs {
		entry := &registeredScope{
			scope:                 def.Scope,
			resourceKind:          def.ResourceKind,
			visibility:            scopeVisibilityUserVisible,
			surface:               GrantSurfaceAccess,
			satisfiedBy:           nil,
			exclusion:             "",
			systemRoles:           def.SystemRoles,
			agentRuntimeSafeSince: def.AgentRuntimeSafeSince,
		}
		if def.Block != nil {
			entry.exclusion = BlockedScope(def.Scope)
			add(&registeredScope{
				scope:                 entry.exclusion,
				resourceKind:          def.ResourceKind,
				visibility:            scopeVisibilityInternal,
				surface:               entry.surface,
				satisfiedBy:           nil,
				exclusion:             "",
				systemRoles:           nil,
				agentRuntimeSafeSince: def.Block.AgentRuntimeSafeSince,
			})
		}
		add(entry)
	}

	// Implications are declared on the higher scope; checks need them on the
	// lower one, as "which grants satisfy a check for this scope".
	for _, def := range defs {
		for _, implied := range def.Implies {
			lower, ok := out[implied]
			if !ok {
				panic(fmt.Sprintf("authz: scope %q implies %q, which is not in the scope registry", def.Scope, implied))
			}
			lower.satisfiedBy = append(lower.satisfiedBy, def.Scope)

			// Mirror onto the blocklist twins: a block on the implied scope
			// also blocks the implying one.
			if lower.exclusion != "" && out[def.Scope].exclusion != "" {
				twin := out[out[def.Scope].exclusion]
				twin.satisfiedBy = append(twin.satisfiedBy, lower.exclusion)
			}
		}
	}
	for _, entry := range out {
		slices.Sort(entry.satisfiedBy)
	}
	return out
}

// withRegistry merges registry-derived values into a legacy table, panicking
// if the legacy table still declares a registry scope.
func withRegistry[V any](table string, legacy map[Scope]V, value func(*registeredScope) V) map[Scope]V {
	merged := maps.Clone(legacy)
	for scope, entry := range registeredScopes {
		if _, ok := legacy[scope]; ok {
			panic(fmt.Sprintf("authz: scope %q is in the scope registry and must be removed from %s", scope, table))
		}
		merged[scope] = value(entry)
	}
	return merged
}

// withRegistryRole prepends the registry scopes a system role holds to its
// legacy scope list.
func withRegistryRole(role string, legacy []Scope) []Scope {
	var scopes []Scope
	for _, def := range scopeRegistry {
		if !slices.Contains(def.SystemRoles, role) {
			continue
		}
		if slices.Contains(legacy, def.Scope) {
			panic(fmt.Sprintf("authz: scope %q is in the scope registry and must be removed from the %s role list", def.Scope, role))
		}
		scopes = append(scopes, def.Scope)
	}
	return append(scopes, legacy...)
}

// AgentRuntimeSafeSince reports the agent runtime scope registry version in
// which scope became safe for agent policy, or zero if it never did. ok is
// false for scopes not yet in the scope registry.
func AgentRuntimeSafeSince(scope Scope) (version int, ok bool) {
	entry, ok := registeredScopes[scope]
	if !ok {
		return 0, false
	}
	return entry.agentRuntimeSafeSince, true
}

// RegistryScopes returns every scope the scope registry declares, including
// derived blocklist twins, in stable order.
func RegistryScopes() []Scope {
	return slices.Sorted(maps.Keys(registeredScopes))
}
