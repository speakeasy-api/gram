package authz

// AgentOwnerSelf names the caller in an agent's owner selector.
const AgentOwnerSelf = "self"

// AgentOwnerSelector covers all agents belonging to the selected owner.
func AgentOwnerSelector(scope Scope, ownerUserID string) Selector {
	selector := NewSelector(scope, WildcardResource)
	selector[SelectorKeyOwner] = ownerUserID
	return selector
}

// AgentCheck includes the persisted owner, just as MCP checks include the
// resource's project or tool. Strict matching retains every grant constraint.
func AgentCheck(scope Scope, agentID, ownerUserID string) Check {
	var dimensions map[string]string
	if ownerUserID != "" {
		dimensions = map[string]string{SelectorKeyOwner: ownerUserID}
	}
	return (Check{
		Scope: scope, ResourceKind: ResourceKindAgent,
		ResourceID: agentID, Dimensions: dimensions, selectorMatch: selectorMatchNormal,
	}).WithStrictSelectorMatch()
}
