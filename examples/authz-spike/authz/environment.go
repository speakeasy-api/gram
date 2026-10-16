package authz

const (
	// EnvironmentRead permits reading an environment, including secret values.
	EnvironmentRead Permission = "environment:read"

	// EnvironmentWrite permits editing environments and creating them in a project.
	EnvironmentWrite Permission = "environment:write"

	// EnvironmentBlockedRead opposes environment read and write.
	EnvironmentBlockedRead Permission = "environment:blocked_read"

	// EnvironmentBlockedWrite opposes write without removing read.
	EnvironmentBlockedWrite Permission = "environment:blocked_write"
)

// EnvironmentDefinitions keeps secret access independent of project permissions.
func EnvironmentDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: EnvironmentRead, Resource: Environment,
			AllowedAnchors: []ResourceKind{Environment, Project, Organization},
			ImpliedBy:      []Permission{EnvironmentWrite}, ExcludedBy: []Permission{EnvironmentBlockedRead},
			Forms: []OperationForm{ResourceOperation}, RequiredAncestors: []ResourceKind{Project},
		},
		{
			Name: EnvironmentWrite, Resource: Environment,
			AllowedAnchors:    []ResourceKind{Environment, Project, Organization},
			ExcludedBy:        []Permission{EnvironmentBlockedWrite, EnvironmentBlockedRead},
			Forms:             []OperationForm{ResourceOperation, CreationOperation},
			RequiredAncestors: []ResourceKind{Project}, CreationParent: Project,
		},
		{
			Name: EnvironmentBlockedRead, Resource: Environment,
			AllowedAnchors: []ResourceKind{Environment, Project, Organization},
		},
		{
			Name: EnvironmentBlockedWrite, Resource: Environment,
			AllowedAnchors: []ResourceKind{Environment, Project, Organization},
		},
	}
}
