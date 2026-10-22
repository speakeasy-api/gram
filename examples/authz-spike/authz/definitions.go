package authz

// AllDefinitions returns the full permission catalog currently modeled here.
// Definitions describe permissions; they assign no grants or role defaults.
func AllDefinitions() []PermissionDefinition {
	definitions := []PermissionDefinition{}
	for _, family := range [][]PermissionDefinition{
		OrganizationDefinitions(), ProjectDefinitions(), MCPDefinitions(),
		EnvironmentDefinitions(), SkillDefinitions(), AssistantDefinitions(),
		PluginDefinitions(), ChatDefinitions(), AgentDefinitions(),
	} {
		definitions = append(definitions, family...)
	}
	return definitions
}
