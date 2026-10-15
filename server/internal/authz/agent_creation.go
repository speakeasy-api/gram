package authz

// AgentOwnerSelf names the caller as the intended owner of a new agent.
const AgentOwnerSelf = "self"

// AgentCreationChecks names the intended owner in the creation-only namespace.
// The self alternative is available only when the caller is the intended owner.
// Both checks use the normal scope-and-selector evaluator.
func AgentCreationChecks(callerUserID, ownerUserID string) []Check {
	checks := []Check{{
		Scope: ScopeAgentCreate, ResourceKind: ResourceKindAgentOwner,
		ResourceID: ownerUserID, Dimensions: nil, selectorMatch: selectorMatchAgentOwnerID,
	}}
	if callerUserID == ownerUserID {
		checks = append(checks, Check{
			Scope: ScopeAgentCreate, ResourceKind: ResourceKindAgentOwner,
			ResourceID: AgentOwnerSelf, Dimensions: nil, selectorMatch: selectorMatchStrict,
		})
	}
	return checks
}
