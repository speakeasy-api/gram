package platformtools

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/stretchr/testify/require"
)

type fakeExecutor struct {
	desc core.ToolDescriptor
}

func (f *fakeExecutor) Descriptor() core.ToolDescriptor { return f.desc }
func (f *fakeExecutor) Call(_ context.Context, _ toolconfig.ToolCallEnv, _ io.Reader, _ io.Writer) error {
	return nil
}

func makeFake(name string, hidden bool) ExternalTool {
	return ExternalTool{
		Executor: &fakeExecutor{
			desc: core.ToolDescriptor{
				SourceSlug:  "test",
				HandlerName: name,
				Name:        name,
				Description: "",
				InputSchema: nil,
				Variables:   nil,
				Annotations: nil,
				Managed:     false,
				OwnerKind:   nil,
				OwnerID:     nil,
				Hidden:      hidden,
			},
		},
		RequiredFeature: "",
	}
}

func descriptorsContain(descs []core.ToolDescriptor, handler string) bool {
	for _, d := range descs {
		if d.HandlerName == handler {
			return true
		}
	}
	return false
}

func typedToolsContainURN(tools []*types.Tool, target string) bool {
	for _, tt := range tools {
		if tt.PlatformToolDefinition != nil && tt.PlatformToolDefinition.ToolUrn == target {
			return true
		}
	}
	return false
}

func TestListPlatformTools_HiddenFilter(t *testing.T) {
	t.Parallel()

	visible := makeFake("test_visible", false)
	hidden := makeFake("test_hidden", true)

	require.False(t, descriptorsContain(ListPlatformTools(false, visible, hidden), "test_hidden"),
		"hidden tool must be excluded when includeHidden=false")
	require.True(t, descriptorsContain(ListPlatformTools(true, visible, hidden), "test_hidden"),
		"hidden tool must be included when includeHidden=true")
	require.True(t, descriptorsContain(ListPlatformTools(false, visible, hidden), "test_visible"),
		"non-hidden tool must always be returned")
}

func TestListTypedTools_HiddenFilter(t *testing.T) {
	t.Parallel()

	visible := makeFake("test_visible", false)
	hidden := makeFake("test_hidden", true)
	projectID := uuid.New()
	hiddenURN := hidden.Executor.Descriptor().ToolURN().String()

	require.False(t, typedToolsContainURN(
		ListTypedTools(t.Context(), "org", projectID, "", false, nil, visible, hidden),
		hiddenURN,
	), "hidden tool must be excluded when includeHidden=false")
	require.True(t, typedToolsContainURN(
		ListTypedTools(t.Context(), "org", projectID, "", true, nil, visible, hidden),
		hiddenURN,
	), "hidden tool must be included when includeHidden=true")
}

func TestFindToolDescriptor_FindsHidden(t *testing.T) {
	t.Parallel()

	hidden := makeFake("test_hidden", true)
	hiddenURN := hidden.Executor.Descriptor().ToolURN()

	desc, ok := FindToolDescriptor(hiddenURN, hidden)
	require.True(t, ok, "FindToolDescriptor must locate hidden tools so execution still works")
	require.True(t, desc.Hidden)
	require.Equal(t, "test_hidden", desc.HandlerName)
}
