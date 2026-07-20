package platformtools

import (
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type fixedDescriptorExecutor struct {
	descriptor ToolDescriptor
}

func (e *fixedDescriptorExecutor) Descriptor() ToolDescriptor {
	return e.descriptor
}

func (e *fixedDescriptorExecutor) Call(_ context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, _ io.Writer) error {
	return nil
}

func TestFindToolEntries(t *testing.T) {
	t.Parallel()

	searchLogsURN := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs").String()
	httpToolURN := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets").String()

	entries := FindToolEntries([]string{
		httpToolURN,
		"not-a-tool-urn",
		searchLogsURN,
		searchLogsURN,
	})

	require.Len(t, entries, 1)
	require.Equal(t, searchLogsURN, entries[0].ToolUrn)
	require.Equal(t, "platform_search_logs", entries[0].Name)
}

func TestFindTypedTools(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	searchLogsURN := urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs").String()
	httpToolURN := urn.NewTool(urn.ToolKindHTTP, "petstore", "list_pets").String()

	tools := FindTypedTools(projectID, []string{
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

	entries := FindToolEntries(
		[]string{builtIn.ToolURN().String()},
		ExternalTool{
			Executor:        &fixedDescriptorExecutor{descriptor: override},
			RequiredFeature: "",
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
		entries := FindToolEntries(toolURNs)
		if len(entries) != 0 {
			b.Fatalf("expected no platform tools, got %d", len(entries))
		}
	}
}
