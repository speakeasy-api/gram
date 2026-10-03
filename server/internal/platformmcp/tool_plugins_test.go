package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// pluginReadToolNames are the plugin tools the managed assistant may call.
var pluginReadToolNames = []string{"list_plugins", "get_plugin", "list_plugin_assignments"}

// pluginMutationToolNames change who receives a plugin or what it carries and
// stay external-only.
var pluginMutationToolNames = []string{operationSetPluginAssignments, operationRepublishPlugin, "distribute_mcp_to_plugin", "remove_mcp_from_plugin"}

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

	_, registrar := newTestServer(t)
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

// requireRepublishPluginDeclaration asserts the contract both republish_plugin
// registrations share: an organization-admin write on the external surface,
// scoped to an explicit project, that is idempotent and never destructive.
func requireRepublishPluginDeclaration(t *testing.T, registrar *Registrar) {
	t.Helper()

	descriptor := descriptorByName(t, registrar, operationRepublishPlugin)
	require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
	require.Equal(t, externalOnly, descriptor.Meta.Audiences)
	require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
	require.NotNil(t, descriptor.Annotations)
	require.False(t, descriptor.Annotations.ReadOnlyHint)
	require.True(t, descriptor.Annotations.IdempotentHint)
	require.NotNil(t, descriptor.Annotations.DestructiveHint)
	require.False(t, *descriptor.Annotations.DestructiveHint)
	for _, assistant := range registrar.For(AudienceAssistant) {
		require.NotEqual(t, operationRepublishPlugin, assistant.Name, "republishing stays off the assistant surface")
	}
}

func TestRepublishPluginToolResultCarriesTheDashboardLink(t *testing.T) {
	t.Parallel()

	result, ok := republishPluginToolResult(&PluginRepublishError{Code: "not_configured", Message: "connect a repository", DashboardURL: "https://app.example.test/org/projects/project/plugins", Cause: ErrPluginRepublishNotConfigured})
	require.True(t, ok)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var refusal pluginRepublishRefusalResult
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, pluginRepublishRefusalResult{Code: "not_configured", Message: "connect a repository", DashboardURL: "https://app.example.test/org/projects/project/plugins"}, refusal)

	unavailable, ok := republishPluginToolResult(pluginRepublishUnavailable(context.Canceled))
	require.True(t, ok)
	text, ok = unavailable.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, unavailableCode)
}
