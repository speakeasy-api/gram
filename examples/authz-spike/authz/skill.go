package authz

const (
	// SkillRead permits reading an existing skill.
	SkillRead Permission = "skill:read"

	// SkillWrite permits editing skills and creating them within a project.
	SkillWrite Permission = "skill:write"

	// SkillBlockedRead opposes skill read and write.
	SkillBlockedRead Permission = "skill:blocked_read"

	// SkillBlockedWrite opposes skill write without removing read.
	SkillBlockedWrite Permission = "skill:blocked_write"
)

// SkillDefinitions declares resource access and project-parent creation.
// Skill access does not imply project access or permission to distribute a skill.
func SkillDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: SkillRead, Resource: Skill,
			AllowedAnchors: []ResourceKind{Skill, Project, Organization},
			ImpliedBy:      []Permission{SkillWrite}, ExcludedBy: []Permission{SkillBlockedRead},
			Forms: []OperationForm{ResourceOperation}, RequiredAncestors: []ResourceKind{Project},
		},
		{
			Name: SkillWrite, Resource: Skill,
			AllowedAnchors:    []ResourceKind{Skill, Project, Organization},
			ExcludedBy:        []Permission{SkillBlockedWrite, SkillBlockedRead},
			Forms:             []OperationForm{ResourceOperation, CreationOperation},
			RequiredAncestors: []ResourceKind{Project}, CreationParent: Project,
		},
		{
			Name: SkillBlockedRead, Resource: Skill,
			AllowedAnchors: []ResourceKind{Skill, Project, Organization},
		},
		{
			Name: SkillBlockedWrite, Resource: Skill,
			AllowedAnchors: []ResourceKind{Skill, Project, Organization},
		},
	}
}
