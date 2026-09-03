package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBuildTopLevelDynamicTools_IncludesPinnedHTTPTool(t *testing.T) {
	t.Parallel()

	pinnedURN := urn.NewTool(urn.ToolKindHTTP, "search_documentation", "abc12345")
	otherURN := urn.NewTool(urn.ToolKindHTTP, "other_tool", "def67890")
	toolset := &types.Toolset{
		ID:               uuid.New().String(),
		TopLevelToolUrns: []string{pinnedURN.String()},
		Tools: []*types.Tool{
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "search_documentation",
					Description: "Search product documentation",
					ToolUrn:     pinnedURN.String(),
					Schema:      `{"type":"object","properties":{"query":{"type":"string"}}}`,
				},
			},
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "other_tool",
					Description: "A hidden dynamic tool",
					ToolUrn:     otherURN.String(),
					Schema:      `{"type":"object"}`,
				},
			},
		},
	}

	entries, err := buildTopLevelDynamicTools(
		t.Context(),
		testenv.NewLogger(t),
		nil,
		nil,
		nil,
		&mcpInputs{},
		toolset,
		nil,
	)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "search_documentation", entries[0].Name)
	require.Contains(t, entries[0].Description, "documentation")
	require.NotEmpty(t, entries[0].InputSchema)
}

func TestBuildTopLevelDynamicTools_EmptyWhenUnset(t *testing.T) {
	t.Parallel()

	toolURN := urn.NewTool(urn.ToolKindHTTP, "search_documentation", "abc12345")
	toolset := &types.Toolset{
		ID:               uuid.New().String(),
		TopLevelToolUrns: nil,
		Tools: []*types.Tool{
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        "search_documentation",
					Description: "Search product documentation",
					ToolUrn:     toolURN.String(),
					Schema:      `{"type":"object"}`,
				},
			},
		},
	}

	entries, err := buildTopLevelDynamicTools(
		t.Context(),
		testenv.NewLogger(t),
		nil,
		nil,
		nil,
		&mcpInputs{},
		toolset,
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestBuildTopLevelDynamicTools_SkipsFacadeNameCollision(t *testing.T) {
	t.Parallel()

	toolURN := urn.NewTool(urn.ToolKindHTTP, searchToolsToolName, "abc12345")
	toolset := &types.Toolset{
		ID:               uuid.New().String(),
		TopLevelToolUrns: []string{toolURN.String()},
		Tools: []*types.Tool{
			{
				HTTPToolDefinition: &types.HTTPToolDefinition{
					Name:        searchToolsToolName,
					Description: "Collides with the facade tool",
					ToolUrn:     toolURN.String(),
					Schema:      `{"type":"object"}`,
				},
			},
		},
	}

	entries, err := buildTopLevelDynamicTools(
		t.Context(),
		testenv.NewLogger(t),
		nil,
		nil,
		nil,
		&mcpInputs{},
		toolset,
		nil,
	)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestIsDynamicFacadeToolName(t *testing.T) {
	t.Parallel()

	require.True(t, isDynamicFacadeToolName(searchToolsToolName))
	require.True(t, isDynamicFacadeToolName(describeToolsToolName))
	require.True(t, isDynamicFacadeToolName(executeToolToolName))
	require.True(t, isDynamicFacadeToolName(listToolsToolName))
	require.False(t, isDynamicFacadeToolName("search_documentation"))
}
