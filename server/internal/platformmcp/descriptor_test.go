package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every tool the deployment registers declares an audience. A tool with none
// would be unreachable; a tool admitted by accident would reach a surface
// nobody reviewed it for, which is what the audience model exists to prevent.
func TestEveryRegisteredToolDeclaresAnAudience(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, CatalogDescriptor{})
	descriptors := registrar.Descriptors()
	require.NotEmpty(t, descriptors, "the deployment registers tools even when every dependency is absent")

	for _, descriptor := range descriptors {
		require.NotEmpty(t, descriptor.Meta.Audiences, "tool %q declares no audience", descriptor.Name)
		require.NotEmpty(t, descriptor.InputSchema, "tool %q advertises no input schema", descriptor.Name)
	}

	external := registrar.For(AudienceExternal)
	require.Len(t, external, len(descriptors), "the external endpoint serves the full catalogue today")

	assistant := registrar.For(AudienceAssistant)
	require.NotEmpty(t, assistant, "the assistant audience is admitted to the catalogue")
}

// Admission is per tool, so narrowing one tool must not affect the others.
func TestAudienceFilterSelectsPerTool(t *testing.T) {
	t.Parallel()

	registrar := &Registrar{descriptors: []Descriptor{
		{Name: "both", Meta: ToolMeta{Audiences: []Audience{AudienceExternal, AudienceAssistant}}},
		{Name: "external-only", Meta: ToolMeta{Audiences: []Audience{AudienceExternal}}},
		{Name: "unadmitted", Meta: ToolMeta{}},
	}}

	require.Equal(t, []string{"both", "external-only"}, names(registrar.For(AudienceExternal)))
	require.Equal(t, []string{"both"}, names(registrar.For(AudienceAssistant)))
}

func names(descriptors []Descriptor) []string {
	out := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		out = append(out, descriptor.Name)
	}
	return out
}

// A tool admitted to the assistant must work without an OAuth connection.
// Admitting one that reaches connection-scoped state advertises a capability
// that always errors, which reads to a user as the platform being broken
// rather than the tool being unavailable.
func TestAssistantAudienceExcludesConnectionScopedTools(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, CatalogDescriptor{})

	admitted := map[string]bool{}
	for _, descriptor := range registrar.For(AudienceAssistant) {
		admitted[descriptor.Name] = true
	}

	// Each of these still reaches state keyed by a connection. Remove a name
	// here in the same change that makes its path connection-less.
	for _, name := range []string{
		"get_mcp_readiness",
		"get_mcp_repair_plan",
		"register_platform_mcp_for_project",
		"get_platform_mcp_onboarding_status",
		"attach_platform_mcp_identity_provider",
		"add_platform_mcp_to_default_plugin",
	} {
		require.False(t, admitted[name], "tool %q needs a connection and must not be admitted to the assistant", name)
	}

	// The reads, the registration path, and the catalogue-to-setup path are
	// connection-less end to end. get_setup_handoff is admitted because the
	// handoff only carries the caller to the dashboard, which completes setup
	// under its own session.
	for _, name := range []string{
		"get_platform_context",
		"list_projects",
		"list_project_mcps",
		"get_mcp",
		"register_catalog_mcp",
		"search_mcp_catalog",
		"inspect_mcp_candidate",
		"send_platform_mcp_feedback",
		"get_setup_handoff",
	} {
		require.True(t, admitted[name], "tool %q works without a connection and should serve the assistant", name)
	}
}
