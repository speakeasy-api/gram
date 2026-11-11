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
	RuntimeScopeRegistryVersion7 RuntimeScopeRegistryVersion = 7

	// RuntimeScopeRegistryVersion8 registers the same scopes as version 7. It
	// exists for DelegatedPolicyVersion8, whose implication closure no longer
	// derives mcp:connect from mcp:read or mcp:write.
	RuntimeScopeRegistryVersion8 RuntimeScopeRegistryVersion = 8

	CurrentRuntimeScopeRegistryVersion = RuntimeScopeRegistryVersion8
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
// and delegated policy may use. It is derived from the authz model, where each
// permission declares since which registry version it is agent-safe and
// retired scopes stay as tombstones.
var runtimeScopeDefinitions = deriveRuntimeScopeDefinitions()

func deriveRuntimeScopeDefinitions() map[authz.Scope]runtimeScopeDefinition {
	declared := authz.AgentEligibilityByScope()
	out := make(map[authz.Scope]runtimeScopeDefinition, len(declared))
	for scope, eligibility := range declared {
		switch {
		case eligibility.Retired:
			out[scope] = retiredRuntimeScope()
		case eligibility.SafeSince == 0:
			out[scope] = activeRuntimeScope()
		default:
			out[scope] = safeRuntimeScopeSince(RuntimeScopeRegistryVersion(eligibility.SafeSince))
		}
	}
	return out
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
