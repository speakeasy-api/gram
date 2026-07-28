package polar

import (
	"testing"

	polarComponents "github.com/polarsource/polar-go/models/components"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

func TestCatalog_IsTopUpProductID(t *testing.T) {
	t.Parallel()

	c := &Catalog{ProductIDsTopUp: []string{"prod_a", "prod_b"}}

	require.True(t, c.IsTopUpProductID("prod_a"))
	require.True(t, c.IsTopUpProductID("prod_b"))
	require.False(t, c.IsTopUpProductID("prod_unknown"))
	require.False(t, c.IsTopUpProductID(""))
}

func TestIsPolarMeteredModelUsageExcludesPlatformInference(t *testing.T) {
	t.Parallel()

	require.False(t, isPolarMeteredModelUsage(billing.ModelUsageSourceGram))
	require.False(t, isPolarMeteredModelUsage(billing.ModelUsageSourceRiskAnalysis))
	require.False(t, isPolarMeteredModelUsage(billing.ModelUsageSourceSkillEfficacy))
	require.True(t, isPolarMeteredModelUsage(billing.ModelUsageSourcePlayground))
}

func TestToolCallUsageMetadata_OmitsEmptyOptionalStringDimensions(t *testing.T) {
	t.Parallel()

	event := billing.ToolCallUsageEvent{
		OrganizationID:        "org_123",
		RequestBytes:          10,
		OutputBytes:           20,
		ToolURN:               "",
		ToolName:              "",
		ResourceURI:           "",
		ProjectID:             "proj_123",
		ProjectSlug:           nil,
		OrganizationSlug:      nil,
		ToolsetSlug:           nil,
		ChatID:                nil,
		MCPURL:                nil,
		Type:                  billing.ToolCallTypeExternalMCP,
		ResponseStatusCode:    200,
		ToolsetID:             nil,
		MCPSessionID:          nil,
		FunctionCPUUsage:      nil,
		FunctionMemUsage:      nil,
		FunctionExecutionTime: nil,
	}

	metadata := toolCallUsageMetadata(event)

	require.NotContains(t, metadata, "tool_urn")
	require.NotContains(t, metadata, "tool_name")
	require.NotContains(t, metadata, "resource_uri")
	require.Equal(t, "proj_123", metadataString(t, metadata, "project_id"))
	require.Equal(t, "external-mcp", metadataString(t, metadata, "type"))
}

func TestToolCallUsageMetadata_IncludesNonEmptyStringDimensions(t *testing.T) {
	t.Parallel()

	event := billing.ToolCallUsageEvent{
		OrganizationID:        "org_123",
		RequestBytes:          10,
		OutputBytes:           20,
		ToolURN:               "gram://toolsets/ts_123/tools/search_tickets",
		ToolName:              "search_tickets",
		ResourceURI:           "ui://widget/ticket-list",
		ProjectID:             "proj_123",
		ProjectSlug:           nil,
		OrganizationSlug:      nil,
		ToolsetSlug:           nil,
		ChatID:                nil,
		MCPURL:                nil,
		Type:                  billing.ToolCallTypeExternalMCP,
		ResponseStatusCode:    200,
		ToolsetID:             nil,
		MCPSessionID:          nil,
		FunctionCPUUsage:      nil,
		FunctionMemUsage:      nil,
		FunctionExecutionTime: nil,
	}

	metadata := toolCallUsageMetadata(event)

	require.Equal(t, "gram://toolsets/ts_123/tools/search_tickets", metadataString(t, metadata, "tool_urn"))
	require.Equal(t, "search_tickets", metadataString(t, metadata, "tool_name"))
	require.Equal(t, "ui://widget/ticket-list", metadataString(t, metadata, "resource_uri"))
}

func metadataString(t *testing.T, metadata map[string]polarComponents.EventMetadataInput, key string) string {
	t.Helper()

	value, ok := metadata[key]
	require.True(t, ok)
	require.NotNil(t, value.Str)

	return *value.Str
}
