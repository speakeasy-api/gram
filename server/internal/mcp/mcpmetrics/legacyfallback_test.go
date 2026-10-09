package mcpmetrics

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyFallbackAttributionWithoutMetrics(t *testing.T) {
	t.Parallel()
	for _, record := range []struct {
		name  string
		write func(*slog.Logger, ToolsetSlugFallback)
	}{
		{"nil metrics", func(logger *slog.Logger, hit ToolsetSlugFallback) {
			var metrics *Metrics
			metrics.RecordToolsetSlugFallback(t.Context(), logger, hit)
		}},
		{"nil counter", func(logger *slog.Logger, hit ToolsetSlugFallback) {
			var counter *LegacyFallbackCounter
			counter.RecordToolsetSlugFallback(t.Context(), logger, hit)
		}},
	} {
		t.Run(record.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			record.write(logger, ToolsetSlugFallback{EntryPoint: LegacyFallbackServePublic, Slug: "legacy-test"})
			require.Contains(t, logs.String(), `"gram.toolset.mcp_slug":"legacy-test"`)
			require.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("\n")))
			require.NotPanics(t, func() { record.write(nil, ToolsetSlugFallback{}) })
		})
	}
}
