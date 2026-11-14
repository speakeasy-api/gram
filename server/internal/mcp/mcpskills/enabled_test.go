package mcpskills_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
)

func TestEnabled(t *testing.T) {
	t.Parallel()

	entitled := func(want bool, calls *int, gotFeature *string) mcpskills.FeatureChecker {
		return func(_ context.Context, _ string, feature string) bool {
			*calls++
			*gotFeature = feature
			return want
		}
	}

	t.Run("product feature on", func(t *testing.T) {
		t.Parallel()
		var calls int
		var got string
		require.True(t, mcpskills.Enabled(t.Context(), entitled(true, &calls, &got), "org_1"))
		require.Equal(t, 1, calls)
		require.Equal(t, "skills_over_mcp", got)
	})

	t.Run("product feature off", func(t *testing.T) {
		t.Parallel()
		var calls int
		var got string
		require.False(t, mcpskills.Enabled(t.Context(), entitled(false, &calls, &got), "org_1"))
	})

	t.Run("no checker", func(t *testing.T) {
		t.Parallel()
		require.False(t, mcpskills.Enabled(t.Context(), nil, "org_1"))
	})

	t.Run("no organization", func(t *testing.T) {
		t.Parallel()
		var calls int
		var got string
		require.False(t, mcpskills.Enabled(t.Context(), entitled(true, &calls, &got), ""))
		require.Zero(t, calls)
	})
}
