package platformtools_test

import (
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type fixedDescriptorExecutor struct {
	descriptor platformtools.ToolDescriptor
}

func (e *fixedDescriptorExecutor) Descriptor() platformtools.ToolDescriptor {
	return e.descriptor
}

func (e *fixedDescriptorExecutor) Call(_ context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, _ io.Writer) error {
	return nil
}

func TestFindToolEntries(t *testing.T) {
	t.Parallel()

	searchLogsURN := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs").String()
	httpToolURN := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets").String()

	entries := platformtools.FindToolEntries([]string{
		httpToolURN,
		"not-a-tool-urn",
		searchLogsURN,
		searchLogsURN,
	})

	require.Len(t, entries, 1)
	require.Equal(t, searchLogsURN, entries[0].ToolUrn)
	require.Equal(t, "platform_search_logs", entries[0].Name)
}

func TestCountTools(t *testing.T) {
	t.Parallel()

	searchLogsURN := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs").String()

	require.Equal(t, 1, platformtools.CountTools([]string{
		urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets").String(),
		urn.NewTool(urn.ToolKindPlatform, "nowhere", "missing").String(),
		"not-a-tool-urn",
		searchLogsURN,
		searchLogsURN,
	}))
	require.Zero(t, platformtools.CountTools(nil))
}

func TestFindTypedTools(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	searchLogsURN := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs").String()
	httpToolURN := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets").String()

	tools := platformtools.FindTypedTools(projectID, []string{
		httpToolURN,
		"not-a-tool-urn",
		searchLogsURN,
		searchLogsURN,
	})

	require.Len(t, tools, 1)
	require.NotNil(t, tools[0].PlatformToolDefinition)
	require.Equal(t, searchLogsURN, tools[0].PlatformToolDefinition.ToolUrn)
	require.Equal(t, projectID.String(), tools[0].PlatformToolDefinition.ProjectID)
}

func TestFindToolEntries_PreservesBuiltInPrecedence(t *testing.T) {
	t.Parallel()

	builtIn := logs.NewSearchLogsTool(nil).Descriptor()
	override := builtIn
	override.Name = "external_override"

	entries := platformtools.FindToolEntries(
		[]string{builtIn.ToolURN().String()},
		platformtools.ExternalTool{
			Executor:        &fixedDescriptorExecutor{descriptor: override},
			RequiredFeature: "",
			RequiredScope:   "",
		},
	)

	require.Len(t, entries, 1)
	require.Equal(t, builtIn.Name, entries[0].Name)
}

func BenchmarkFindToolEntriesWithHTTPTools(b *testing.B) {
	toolURNs := make([]string, 1000)
	for i := range toolURNs {
		toolURNs[i] = urn.NewTool(urn.ToolKindHTTP, "benchmark", "tool_"+strconv.Itoa(i)).String()
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		entries := platformtools.FindToolEntries(toolURNs)
		if len(entries) != 0 {
			b.Fatalf("expected no platform tools, got %d", len(entries))
		}
	}
}

// BuildToolsets must expose the Platform MCP read toolset under its reserved
// slug so the serve path and attachment logic agree on where the tools live.
func TestBuildToolsetsIncludesPlatformMCPReadToolset(t *testing.T) {
	t.Parallel()

	tool := platformtools.ExternalTool{
		Executor: &fixedDescriptorExecutor{descriptor: platformtools.ToolDescriptor{
			SourceSlug:  platformtools.SourcePlatform,
			HandlerName: "list_projects",
			Name:        platformtools.ToolNameListProjects,
		}},
		RequiredFeature: "",
		RequiredScope:   "",
	}

	toolsets := platformtools.BuildToolsets(platformtools.ToolsetDependencies{
		AssistantMemoryTools:          nil,
		AssistantSkillTools:           nil,
		AssistantTriggerTools:         nil,
		ManagedAssistantInsightsTools: nil,
		PlatformMCPReadTools:          []platformtools.ExternalTool{tool},
	})

	ts, ok := toolsets[platformtools.PlatformMCPReadToolsetSlug]
	require.True(t, ok, "platform toolset must be registered")
	require.Len(t, ts.Tools, 1)
	require.Equal(t, platformtools.ToolNameListProjects, ts.Tools[0].Executor.Descriptor().Name)
}

func TestPlatformToolsetIdentityIsStableAndDistinct(t *testing.T) {
	t.Parallel()

	assistantsID := platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug)
	require.Equal(t, assistantsID, platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug))
	require.NotEqual(t, assistantsID, platformtools.PlatformToolsetID(platformtools.ManagedAssistantPlatformToolsetSlug))
	require.Equal(t, "Speakeasy assistant tools", platformtools.PlatformToolsetName(platformtools.AssistantsPlatformToolsetSlug))
}
