package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// pluginReadToolNames are the plugin tools the managed assistant may call.
var pluginReadToolNames = []string{"list_plugins", "get_plugin", "list_plugin_assignments"}

// pluginMutationToolNames change who receives a plugin, what it carries, or
// what it is called, and stay external-only.
var pluginMutationToolNames = []string{operationSetPluginAssignments, operationCreatePlugin, operationRenamePlugin, "distribute_mcp_to_plugin", "remove_mcp_from_plugin"}

// requirePluginToolAudiences asserts the audience split every plugin tool
// registration must keep, whether the plugins service is composed or absent.
func requirePluginToolAudiences(t *testing.T, registrar *Registrar) {
	t.Helper()

	for _, name := range pluginReadToolNames {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, bothAudiences, descriptor.Meta.Audiences, "plugin read %q serves the assistant so it can resolve a plugin by name", name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, "the assistant adapter injects the project for %q", name)
		require.NotNil(t, descriptor.Annotations, name)
		require.True(t, descriptor.Annotations.ReadOnlyHint, name)
	}
	for _, name := range pluginMutationToolNames {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, "plugin mutation %q stays external-only", name)
	}
}

// The unavailable registration keeps the same audiences as the live one, so an
// assistant in an organization without plugins gets a readable refusal rather
// than a tool that appears only once the feature is switched on.
func TestUnavailablePluginToolsKeepReadsForBothAudiencesAndMutationsExternal(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	requirePluginToolAudiences(t, registrar)

	assistant := map[string]bool{}
	for _, descriptor := range registrar.For(AudienceAssistant) {
		assistant[descriptor.Name] = true
	}
	for _, name := range pluginReadToolNames {
		require.True(t, assistant[name], "assistant catalogue lists %q", name)
	}
	for _, name := range pluginMutationToolNames {
		require.False(t, assistant[name], "assistant catalogue must not list %q", name)
	}
}
