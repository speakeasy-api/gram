package authz

import "maps"

// DerivedAgentEligibility is one scope's agent eligibility as the model
// declares it.
type DerivedAgentEligibility struct {
	// SafeSince is the agent runtime scope registry version the scope became
	// agent-safe in, or 0 if it never did.
	SafeSince int
	// Retired marks a tombstone: the scope no longer exists.
	Retired bool
}

// AgentEligibilityByScope returns the agent eligibility of every scope the
// model knows, retired tombstones included. The agent runtime scope registry
// reads it.
func AgentEligibilityByScope() map[Scope]DerivedAgentEligibility {
	return maps.Clone(agentEligibilityByScope)
}
