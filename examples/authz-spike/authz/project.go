package authz

const (
	// ProjectRead permits reading an existing project.
	ProjectRead Permission = "project:read"

	// ProjectWrite permits editing an existing project and implies project read.
	ProjectWrite Permission = "project:write"

	// ProjectBlockedRead opposes project read and write.
	ProjectBlockedRead Permission = "project:blocked_read"

	// ProjectBlockedWrite opposes project write only.
	ProjectBlockedWrite Permission = "project:blocked_write"
)

// ProjectDefinitions separates existing-project operations from organization
// administration. Project creation requires OrgAdmin on the organization instead.
func ProjectDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: ProjectRead, Resource: Project,
			AllowedAnchors: []ResourceKind{Project, Organization},
			ImpliedBy:      []Permission{ProjectWrite}, ExcludedBy: []Permission{ProjectBlockedRead},
			Forms: []OperationForm{ResourceOperation},
		},
		{
			Name: ProjectWrite, Resource: Project,
			AllowedAnchors: []ResourceKind{Project, Organization},
			ExcludedBy:     []Permission{ProjectBlockedWrite, ProjectBlockedRead},
			Forms:          []OperationForm{ResourceOperation},
		},
		{
			Name: ProjectBlockedRead, Resource: Project,
			AllowedAnchors: []ResourceKind{Project, Organization},
		},
		{
			Name: ProjectBlockedWrite, Resource: Project,
			AllowedAnchors: []ResourceKind{Project, Organization},
		},
	}
}
