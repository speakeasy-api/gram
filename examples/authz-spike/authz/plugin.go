package authz

const (
	// PluginWrite permits plugin content management, not editing referenced resources.
	PluginWrite Permission = "plugin:write"

	// PluginBlockedWrite opposes plugin content management.
	PluginBlockedWrite Permission = "plugin:blocked_write"
)

// PluginDefinitions declares explicit plugin write and project-parent creation.
// Organization-admin alternatives and inventory reads belong to domain composition;
// neither is a cross-resource implication or an invented plugin:read scope.
func PluginDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: PluginWrite, Resource: Plugin,
			AllowedAnchors:    []ResourceKind{Plugin, Project, Organization},
			ExcludedBy:        []Permission{PluginBlockedWrite},
			Forms:             []OperationForm{ResourceOperation, CreationOperation},
			RequiredAncestors: []ResourceKind{Project}, CreationParent: Project,
		},
		{
			Name: PluginBlockedWrite, Resource: Plugin,
			AllowedAnchors: []ResourceKind{Plugin, Project, Organization},
		},
	}
}
