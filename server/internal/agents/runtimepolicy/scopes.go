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

	CurrentRuntimeScopeRegistryVersion = RuntimeScopeRegistryVersion1
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
	return runtimeScopeDefinition{
		lifecycle: RuntimeScopeLifecycleActive,
		safeSince: RuntimeScopeRegistryVersion1,
	}
}

func retiredRuntimeScope() runtimeScopeDefinition {
	return runtimeScopeDefinition{
		lifecycle: RuntimeScopeLifecycleRetired,
		safeSince: 0,
	}
}

// runtimeScopeDefinitions is the application-owned overlay that determines
// which authorization scopes agent direct and delegated policy may use.
var runtimeScopeDefinitions = map[authz.Scope]runtimeScopeDefinition{
	authz.ScopeRoot:                    activeRuntimeScope(),
	authz.ScopeOrgRead:                 activeRuntimeScope(),
	authz.ScopeOrgBlockedRead:          activeRuntimeScope(),
	authz.ScopeOrgAdmin:                activeRuntimeScope(),
	authz.ScopeOrgBlockedAdmin:         activeRuntimeScope(),
	authz.ScopeProjectRead:             safeRuntimeScope(),
	authz.ScopeProjectBlockedRead:      activeRuntimeScope(),
	authz.ScopeProjectWrite:            safeRuntimeScope(),
	authz.ScopeProjectBlockedWrite:     activeRuntimeScope(),
	authz.ScopeMCPRead:                 safeRuntimeScope(),
	authz.ScopeMCPBlockedRead:          activeRuntimeScope(),
	authz.ScopeMCPWrite:                safeRuntimeScope(),
	authz.ScopeMCPBlockedWrite:         activeRuntimeScope(),
	authz.ScopeMCPConnect:              safeRuntimeScope(),
	authz.ScopeMCPBlockedConnect:       activeRuntimeScope(),
	authz.ScopeEnvironmentRead:         safeRuntimeScope(),
	authz.ScopeEnvironmentBlockedRead:  activeRuntimeScope(),
	authz.ScopeEnvironmentWrite:        safeRuntimeScope(),
	authz.ScopeEnvironmentBlockedWrite: activeRuntimeScope(),
	authz.ScopeSkillRead:               safeRuntimeScope(),
	authz.ScopeSkillBlockedRead:        activeRuntimeScope(),
	authz.ScopeSkillWrite:              safeRuntimeScope(),
	authz.ScopeSkillBlockedWrite:       activeRuntimeScope(),
	authz.ScopeRiskPolicyEvaluate:      safeRuntimeScope(),
	authz.ScopeRiskPolicyBypass:        activeRuntimeScope(),
	authz.ScopeRiskPolicyBlock:         activeRuntimeScope(),
	authz.ScopeChatRead:                activeRuntimeScope(),
	authz.ScopeChatWrite:               activeRuntimeScope(),
	authz.ScopeAgentRead:               activeRuntimeScope(),
	authz.ScopeAgentWrite:              activeRuntimeScope(),
	authz.ScopeAgentAuthorize:          activeRuntimeScope(),
	authz.ScopeAgentTransfer:           activeRuntimeScope(),
	scopeMCPApprovalReadTombstone:      retiredRuntimeScope(),
	scopeMCPApprovalDecideTombstone:    retiredRuntimeScope(),
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
