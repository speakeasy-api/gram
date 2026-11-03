package runtimepolicy

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// RuntimeScopeRegistryVersion identifies an immutable version of the
// application-owned allowlist used by agent direct and delegated policy.
type RuntimeScopeRegistryVersion int

const (
	RuntimeScopeRegistryVersion1 RuntimeScopeRegistryVersion = 1
	RuntimeScopeRegistryVersion2 RuntimeScopeRegistryVersion = 2
	RuntimeScopeRegistryVersion3 RuntimeScopeRegistryVersion = 3
	RuntimeScopeRegistryVersion4 RuntimeScopeRegistryVersion = 4
	RuntimeScopeRegistryVersion5 RuntimeScopeRegistryVersion = 5

	RuntimeScopeRegistryVersion6 RuntimeScopeRegistryVersion = 6

	CurrentRuntimeScopeRegistryVersion = RuntimeScopeRegistryVersion6
)

// RuntimeScopeLifecycle distinguishes active scope registrations from retained
// retirement tombstones in the agent runtime scope registry.
type RuntimeScopeLifecycle string

const (
	RuntimeScopeLifecycleActive  RuntimeScopeLifecycle = "active"
	RuntimeScopeLifecycleRetired RuntimeScopeLifecycle = "retired"
)

// Retired scope names remain registered as tombstones so stored delegated
// policy can still be parsed and displayed. Retired names must never be reused.
const (
	scopeMCPApprovalReadTombstone   authz.Scope = "mcp_approval:read"
	scopeMCPApprovalDecideTombstone authz.Scope = "mcp_approval:decide"
)

type runtimeScopeDefinition struct {
	lifecycle RuntimeScopeLifecycle
	safeSince RuntimeScopeRegistryVersion
}

func activeRuntimeScope() runtimeScopeDefinition {
	return runtimeScopeDefinition{
		lifecycle: RuntimeScopeLifecycleActive,
		safeSince: 0,
	}
}

func safeRuntimeScope() runtimeScopeDefinition {
	return safeRuntimeScopeSince(RuntimeScopeRegistryVersion1)
}

// safeRuntimeScopeSince registers a scope that became agent-runtime-safe in a
// later registry version, so policies stored at older versions stay unchanged.
func safeRuntimeScopeSince(version RuntimeScopeRegistryVersion) runtimeScopeDefinition {
	return runtimeScopeDefinition{
		lifecycle: RuntimeScopeLifecycleActive,
		safeSince: version,
	}
}

func retiredRuntimeScope() runtimeScopeDefinition {
	return runtimeScopeDefinition{
		lifecycle: RuntimeScopeLifecycleRetired,
		safeSince: 0,
	}
}

// runtimeScopeDefinitions determines which authorization scopes agent direct
// and delegated policy may use. Scopes in the core authz scope registry carry
// their safety there (ScopeDef.AgentRuntimeSafeSince); the literal below holds
// the scopes not migrated yet and the retirement tombstones.
// Scopes in the scope registry are merged in by withRegistryRuntimeScopes, so this
// literal is partial on purpose; TestRuntimeScopeRegistryCoversAuthorizationScopes
// checks coverage in place of the exhaustive linter.
//
//exhaustive:ignore
var runtimeScopeDefinitions = withRegistryRuntimeScopes(map[authz.Scope]runtimeScopeDefinition{
	authz.ScopeRoot:                    activeRuntimeScope(),
	authz.ScopeProjectRead:             safeRuntimeScope(),
	authz.ScopeProjectBlockedRead:      safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeProjectWrite:            safeRuntimeScope(),
	authz.ScopeProjectBlockedWrite:     safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeMCPRead:                 safeRuntimeScope(),
	authz.ScopeMCPBlockedRead:          safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeMCPWrite:                safeRuntimeScope(),
	authz.ScopeMCPBlockedWrite:         safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeMCPConnect:              safeRuntimeScope(),
	authz.ScopeMCPBlockedConnect:       safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeMCPAuth:                 safeRuntimeScopeSince(RuntimeScopeRegistryVersion5),
	authz.ScopeMCPBlockedAuth:          safeRuntimeScopeSince(RuntimeScopeRegistryVersion5),
	authz.ScopeEnvironmentRead:         safeRuntimeScope(),
	authz.ScopeEnvironmentBlockedRead:  safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeEnvironmentWrite:        safeRuntimeScope(),
	authz.ScopeEnvironmentBlockedWrite: safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeSkillRead:               safeRuntimeScope(),
	authz.ScopeSkillBlockedRead:        safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeSkillWrite:              safeRuntimeScope(),
	authz.ScopeSkillBlockedWrite:       safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeAssistantRead:           safeRuntimeScopeSince(RuntimeScopeRegistryVersion3),
	authz.ScopeAssistantBlockedRead:    safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopeAssistantWrite:          safeRuntimeScopeSince(RuntimeScopeRegistryVersion3),
	authz.ScopeAssistantBlockedWrite:   safeRuntimeScopeSince(RuntimeScopeRegistryVersion4),
	authz.ScopePluginWrite:             activeRuntimeScope(),
	authz.ScopePluginBlockedWrite:      activeRuntimeScope(),
	authz.ScopeRiskPolicyEvaluate:      safeRuntimeScope(),
	authz.ScopeRiskPolicyBypass:        activeRuntimeScope(),
	authz.ScopeRiskPolicyBlock:         activeRuntimeScope(),
	authz.ScopeChatRead:                activeRuntimeScope(),
	authz.ScopeChatWrite:               activeRuntimeScope(),
	authz.ScopeInferenceExecute:        safeRuntimeScopeSince(RuntimeScopeRegistryVersion6),
	authz.ScopeLogRead:                 activeRuntimeScope(),
	authz.ScopeAgentRead:               activeRuntimeScope(),
	authz.ScopeAgentWrite:              activeRuntimeScope(),
	authz.ScopeAgentAuthorize:          activeRuntimeScope(),
	authz.ScopeAgentTransfer:           activeRuntimeScope(),
	// Registered but deliberately NOT agent-runtime-safe. A workload inherits its
	// assigned agent's policy, so making these safe would let a machine admit
	// further machines and assign them agents — the trust policy editing itself.
	// Configuring workload identity stays a human act, beside agent:authorize.
	authz.ScopeWorkloadRead:         activeRuntimeScope(),
	authz.ScopeWorkloadBlockedRead:  activeRuntimeScope(),
	authz.ScopeWorkloadWrite:        activeRuntimeScope(),
	authz.ScopeWorkloadBlockedWrite: activeRuntimeScope(),
	authz.ScopeOrgDeviceAgentSync:   safeRuntimeScopeSince(RuntimeScopeRegistryVersion2),
	authz.ScopeOrgHooksIngest:       safeRuntimeScopeSince(RuntimeScopeRegistryVersion2),
	scopeMCPApprovalReadTombstone:   retiredRuntimeScope(),
	scopeMCPApprovalDecideTombstone: retiredRuntimeScope(),
})

// withRegistryRuntimeScopes adds the runtime safety of every scope in the core
// authz scope registry, panicking if legacy still declares one of them.
func withRegistryRuntimeScopes(legacy map[authz.Scope]runtimeScopeDefinition) map[authz.Scope]runtimeScopeDefinition {
	for _, scope := range authz.RegistryScopes() {
		if _, ok := legacy[scope]; ok {
			panic(fmt.Sprintf("runtimepolicy: scope %q is in the authz scope registry and must be removed from runtimeScopeDefinitions", scope))
		}
		safeSince, _ := authz.AgentRuntimeSafeSince(scope)
		legacy[scope] = runtimeScopeDefinition{
			lifecycle: RuntimeScopeLifecycleActive,
			safeSince: RuntimeScopeRegistryVersion(safeSince),
		}
	}
	return legacy
}

// RuntimeScopeLifecycleFor returns the lifecycle of a scope known to the agent
// runtime registry. Unknown scopes return false.
func RuntimeScopeLifecycleFor(scope authz.Scope) (RuntimeScopeLifecycle, bool) {
	definition, ok := runtimeScopeDefinitions[scope]
	if !ok {
		return "", false
	}
	return definition.lifecycle, true
}

// ValidateRuntimeScope verifies that a scope and its complete implication
// closure are active and explicitly safe in the requested registry version.
func ValidateRuntimeScope(version RuntimeScopeRegistryVersion, scope authz.Scope) error {
	if version < RuntimeScopeRegistryVersion1 || version > CurrentRuntimeScopeRegistryVersion {
		return fmt.Errorf("unsupported agent runtime scope registry version %d", version)
	}

	for _, implied := range authz.ScopeImplicationClosure(scope) {
		definition, ok := runtimeScopeDefinitions[implied]
		if !ok {
			return fmt.Errorf("scope %q is not registered", implied)
		}
		if definition.lifecycle != RuntimeScopeLifecycleActive {
			return fmt.Errorf("scope %q is retired", implied)
		}
		if definition.safeSince == 0 || definition.safeSince > version {
			return fmt.Errorf("scope %q is not agent-runtime-safe", implied)
		}
	}

	return nil
}

// IsRuntimeScopeSafe reports whether the scope is valid for direct agent policy
// or delegated credential policy in the requested registry version.
func IsRuntimeScopeSafe(version RuntimeScopeRegistryVersion, scope authz.Scope) bool {
	return ValidateRuntimeScope(version, scope) == nil
}
