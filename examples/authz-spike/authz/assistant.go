package authz

const (
	// AssistantRead permits reading an existing assistant's configuration.
	AssistantRead Permission = "assistant:read"

	// AssistantWrite permits editing assistants and creating them in a project.
	AssistantWrite Permission = "assistant:write"

	// AssistantBlockedRead opposes assistant read and write.
	AssistantBlockedRead Permission = "assistant:blocked_read"

	// AssistantBlockedWrite opposes write without removing read.
	AssistantBlockedWrite Permission = "assistant:blocked_write"
)

// AssistantDefinitions declares configuration access, not runtime tool authority
// or transcript access. Resource ancestry enables enclosing assistant grants.
func AssistantDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: AssistantRead, Resource: Assistant,
			AllowedAnchors: []ResourceKind{Assistant, Project, Organization},
			ImpliedBy:      []Permission{AssistantWrite}, ExcludedBy: []Permission{AssistantBlockedRead},
			Forms: []OperationForm{ResourceOperation}, RequiredAncestors: []ResourceKind{Project},
		},
		{
			Name: AssistantWrite, Resource: Assistant,
			AllowedAnchors:    []ResourceKind{Assistant, Project, Organization},
			ExcludedBy:        []Permission{AssistantBlockedWrite, AssistantBlockedRead},
			Forms:             []OperationForm{ResourceOperation, CreationOperation},
			RequiredAncestors: []ResourceKind{Project}, CreationParent: Project,
		},
		{
			Name: AssistantBlockedRead, Resource: Assistant,
			AllowedAnchors: []ResourceKind{Assistant, Project, Organization},
		},
		{
			Name: AssistantBlockedWrite, Resource: Assistant,
			AllowedAnchors: []ResourceKind{Assistant, Project, Organization},
		},
	}
}
