package mv

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAdaptPlatformToolSchemasCacheIsolation(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	toolURN := urn.NewTool(urn.ToolKindPlatform, "slack", "send_message").String()
	tools := platformtools.FindTypedTools(projectID, []string{toolURN})
	require.Len(t, tools, 1)
	original := tools[0].PlatformToolDefinition
	originalSchema := original.Schema
	original.Name = "preserved-name"
	original.Description = "Preserved description"
	nonPlatform := new(types.Tool)
	nonPlatform.HTTPToolDefinition = new(types.HTTPToolDefinition)
	cached := new(ToolsetBaseContents)
	cached.ToolsetID = uuid.NewString()
	cached.DeploymentID = uuid.NewString()
	cached.Version = 7
	cached.Tools = tools
	cached.Tools = append(cached.Tools, nonPlatform, nil)
	cached.Resources = []*types.Resource{new(types.Resource)}

	check := func(t *testing.T, ctx context.Context, automatic bool) {
		t.Helper()
		result := adaptPlatformToolSchemas(ctx, cached)
		require.NotSame(t, cached, result)
		require.Equal(t, cached.ToolsetID, result.ToolsetID)
		require.Equal(t, cached.DeploymentID, result.DeploymentID)
		require.Equal(t, cached.Version, result.Version)
		require.Equal(t, cached.Resources, result.Resources)
		require.Same(t, nonPlatform, result.Tools[1])
		require.Nil(t, result.Tools[2])
		require.NotSame(t, cached.Tools[0], result.Tools[0])
		definition := result.Tools[0].PlatformToolDefinition
		require.NotSame(t, original, definition)
		require.Equal(t, original.Name, definition.Name)
		require.Equal(t, original.Description, definition.Description)
		require.Equal(t, original.ID, definition.ID)
		require.Equal(t, !automatic, strings.Contains(definition.Schema, `"thread_id"`))
		if automatic {
			require.Equal(t, originalSchema, definition.Schema)
		}

		// The subsequent variation stage changes this request's copy only, and
		// retains its caller-specific schema rather than restoring cached schema.
		variation := new(types.ToolVariation)
		name := "request-only-name"
		variation.Name = &name
		conv.ApplyVariation(*result.Tools[0], *variation)
		require.Equal(t, name, definition.Name)
		require.Equal(t, !automatic, strings.Contains(definition.Schema, `"thread_id"`))
		result.Tools[1] = nil
		require.Same(t, nonPlatform, cached.Tools[1])
		require.Equal(t, "preserved-name", original.Name)
		require.Equal(t, originalSchema, original.Schema)
	}
	automaticCtx := contextvalues.SetAssistantPrincipal(t.Context(), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	// Alternate calls against one cache object before the concurrent cases.
	for range 3 {
		check(t, t.Context(), false)
		check(t, automaticCtx, true)
	}
	for i := range 24 {
		t.Run(fmt.Sprintf("caller-%d", i), func(t *testing.T) {
			t.Parallel()
			if i%2 == 0 {
				check(t, automaticCtx, true)
			} else {
				check(t, t.Context(), false)
			}
		})
	}
}
