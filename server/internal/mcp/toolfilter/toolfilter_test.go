package toolfilter_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
)

func TestResolveGroupID_PrefersMcpServer(t *testing.T) {
	t.Parallel()

	mcpServer := uuid.New()
	toolset := uuid.New()

	got := toolfilter.ResolveGroupID(&mcpServer, &toolset)
	require.NotNil(t, got)
	require.Equal(t, mcpServer, *got)
}

func TestResolveGroupID_FallsBackToToolset(t *testing.T) {
	t.Parallel()

	toolset := uuid.New()

	got := toolfilter.ResolveGroupID(nil, &toolset)
	require.NotNil(t, got)
	require.Equal(t, toolset, *got)
}

func TestResolveGroupID_NoneConfigured(t *testing.T) {
	t.Parallel()

	require.Nil(t, toolfilter.ResolveGroupID(nil, nil))
}

func httpToolWithTags(name string, tags []string) *types.Tool {
	return &types.Tool{
		HTTPToolDefinition: &types.HTTPToolDefinition{
			Name:      name,
			Variation: &types.ToolVariation{Tags: tags},
		},
	}
}

func httpToolWithoutVariation(name string) *types.Tool {
	return &types.Tool{
		HTTPToolDefinition: &types.HTTPToolDefinition{Name: name},
	}
}

func httpToolWithSourceTags(name string, tags []string) *types.Tool {
	return &types.Tool{
		HTTPToolDefinition: &types.HTTPToolDefinition{Name: name, Tags: tags},
	}
}

func toolNames(t *testing.T, tools []*types.Tool) []string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		require.NotNil(t, tool.HTTPToolDefinition)
		names = append(names, tool.HTTPToolDefinition.Name)
	}
	return names
}

func TestFilterToolsByTags_UnionMatch(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{
		httpToolWithTags("a", []string{"billing"}),
		httpToolWithTags("b", []string{"admin"}),
		httpToolWithTags("c", []string{"reporting"}),
	}

	got := toolfilter.FilterToolsByTags(tools, []string{"billing", "admin"})
	require.Equal(t, []string{"a", "b"}, toolNames(t, got))
}

func TestFilterToolsByTags_SourceTagsMatchWithoutVariation(t *testing.T) {
	t.Parallel()

	// A variation is not required: source-defined tags drive filtering on their
	// own.
	tools := []*types.Tool{
		httpToolWithSourceTags("a", []string{"billing"}),
		httpToolWithSourceTags("b", []string{"admin"}),
	}

	got := toolfilter.FilterToolsByTags(tools, []string{"billing"})
	require.Equal(t, []string{"a"}, toolNames(t, got))
}

func TestFilterToolsByTags_VariationTagsReplaceSourceTags(t *testing.T) {
	t.Parallel()

	// A variation that defines tags replaces the source tags entirely.
	tool := &types.Tool{HTTPToolDefinition: &types.HTTPToolDefinition{
		Name:      "a",
		Tags:      []string{"source-only"},
		Variation: &types.ToolVariation{Tags: []string{"variation-only"}},
	}}

	require.Empty(t, toolfilter.FilterToolsByTags([]*types.Tool{tool}, []string{"source-only"}))
	require.Len(t, toolfilter.FilterToolsByTags([]*types.Tool{tool}, []string{"variation-only"}), 1)
}

func TestFilterToolsByTags_NilVariationTagsFallBackToSourceTags(t *testing.T) {
	t.Parallel()

	// A variation present but not modifying tags (nil) leaves the source tags
	// authoritative.
	tool := &types.Tool{HTTPToolDefinition: &types.HTTPToolDefinition{
		Name:      "a",
		Tags:      []string{"billing"},
		Variation: &types.ToolVariation{Tags: nil},
	}}

	require.Len(t, toolfilter.FilterToolsByTags([]*types.Tool{tool}, []string{"billing"}), 1)
}

func TestFilterToolsByTags_EmptyVariationTagsRemoveFromAllFilters(t *testing.T) {
	t.Parallel()

	// An explicit empty set (non-nil, length 0) removes the tool from every tag
	// filter, even though the source defines a matching tag.
	tool := &types.Tool{HTTPToolDefinition: &types.HTTPToolDefinition{
		Name:      "a",
		Tags:      []string{"billing"},
		Variation: &types.ToolVariation{Tags: []string{}},
	}}

	require.Empty(t, toolfilter.FilterToolsByTags([]*types.Tool{tool}, []string{"billing"}))
}

func TestFilterToolsByTags_ExcludesToolsWithoutAnyTags(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{
		httpToolWithSourceTags("a", []string{"billing"}),
		// No variation and no source tags.
		httpToolWithoutVariation("b"),
		// External MCP (proxy) tools never carry tags, so they are excluded
		// whenever a filter is active.
		{ExternalMcpToolDefinition: &types.ExternalMCPToolDefinition{Name: "proxy"}},
	}

	got := toolfilter.FilterToolsByTags(tools, []string{"billing"})
	require.Equal(t, []string{"a"}, toolNames(t, got))
}

func TestFilterToolsByTags_NonexistentTagReturnsEmpty(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{
		httpToolWithTags("a", []string{"billing"}),
		httpToolWithTags("b", []string{"admin"}),
	}

	got := toolfilter.FilterToolsByTags(tools, []string{"does-not-exist"})
	require.Empty(t, got)
}

func TestFilterToolsByTags_CaseSensitive(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{httpToolWithTags("a", []string{"Billing"})}

	require.Empty(t, toolfilter.FilterToolsByTags(tools, []string{"billing"}))
	require.Len(t, toolfilter.FilterToolsByTags(tools, []string{"Billing"}), 1)
}

func TestFilterToolsByTags_ToolWithMultipleTagsMatchesAny(t *testing.T) {
	t.Parallel()

	tools := []*types.Tool{httpToolWithTags("a", []string{"x", "y", "z"})}

	require.Len(t, toolfilter.FilterToolsByTags(tools, []string{"y"}), 1)
}
