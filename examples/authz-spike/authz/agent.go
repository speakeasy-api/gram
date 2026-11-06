package authz

const (
	// AgentRead permits explicit grant-based inspection of an agent identity.
	AgentRead Permission = "agent:read"

	// AgentWrite permits explicit grant-based configuration of an agent identity.
	AgentWrite Permission = "agent:write"

	// AgentAuthorize permits explicit grant-based credential management.
	AgentAuthorize Permission = "agent:authorize"

	// AgentTransfer permits explicit grant-based ownership transfer.
	AgentTransfer Permission = "agent:transfer"
)

// AgentDefinitions declares independent management permissions on tenant-bound
// agents. These describe the target resource, not the executing principal's kind.
// Owner admission, human eligibility, creation, and mutation workflows are separate.
func AgentDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: AgentRead, Resource: Agent,
			AllowedAnchors: []ResourceKind{Agent, Organization},
			Forms:          []OperationForm{ResourceOperation},
		},
		{
			Name: AgentWrite, Resource: Agent,
			AllowedAnchors: []ResourceKind{Agent, Organization},
			Forms:          []OperationForm{ResourceOperation},
		},
		{
			Name: AgentAuthorize, Resource: Agent,
			AllowedAnchors: []ResourceKind{Agent, Organization},
			Forms:          []OperationForm{ResourceOperation},
		},
		{
			Name: AgentTransfer, Resource: Agent,
			AllowedAnchors: []ResourceKind{Agent, Organization},
			Forms:          []OperationForm{ResourceOperation},
		},
	}
}
