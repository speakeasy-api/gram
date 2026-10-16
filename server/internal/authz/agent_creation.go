package authz

// AgentOwnerSelf names the caller in an agent's owner selector.
const AgentOwnerSelf = "self"

// AgentOwnerSelector covers all agents belonging to the selected owner.
func AgentOwnerSelector(scope Scope, ownerUserID string) Selector {
	selector := NewSelector(scope, WildcardResource)
	selector[SelectorKeyOwnerUserID] = ownerUserID
	return selector
}

// AgentCreationChecks requires write access to all agents of the intended owner.
// A grant naming one existing agent cannot authorize creating another.
func AgentCreationChecks(callerUserID, ownerUserID string) []Check {
	return AgentChecks(ScopeAgentWrite, WildcardResource, callerUserID, ownerUserID)
}

// AgentChecks applies the current persisted owner to every agent operation.
// The relative self alternative is available only to that owner.
func AgentChecks(scope Scope, agentID, callerUserID, ownerUserID string) []Check {
	checks := []Check{{
		Scope: scope, ResourceKind: ResourceKindAgent,
		ResourceID: agentID, Dimensions: map[string]string{SelectorKeyOwnerUserID: ownerUserID}, selectorMatch: selectorMatchAgentOwnerID,
	}}
	if callerUserID == ownerUserID {
		checks = append(checks, Check{
			Scope: scope, ResourceKind: ResourceKindAgent,
			ResourceID: agentID, Dimensions: map[string]string{SelectorKeyOwnerUserID: AgentOwnerSelf}, selectorMatch: selectorMatchStrict,
		})
	}
	return checks
}
