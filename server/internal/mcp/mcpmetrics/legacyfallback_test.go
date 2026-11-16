package mcpmetrics

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
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
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			record.write(logger, ToolsetSlugFallback{EntryPoint: LegacyFallbackServePublic, Slug: "legacy-test"})
			require.Contains(t, logs.String(), `"gram.toolset.mcp_slug":"legacy-test"`)
			require.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("\n")))
			require.NotPanics(t, func() { record.write(nil, ToolsetSlugFallback{}) })
		})
	}
}

func TestLegacyAudienceAcceptedLog(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	var counter *LegacyFallbackCounter
	hit := LegacyAudienceAccepted{IssuerID: uuid.New(), Slug: "legacy-a", ToolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, McpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ProjectID: uuid.New(), CustomDomainHost: true}

	counter.RecordLegacyAudienceAccepted(t.Context(), logger, hit)
	counter.RecordLegacyAudienceAccepted(t.Context(), logger, hit)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 2)
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &record))
	require.Equal(t, "mcp bearer accepted via legacy toolset audience", record["msg"])
	require.Equal(t, hit.IssuerID.String(), record[string(attr.UserSessionIssuerIDKey)])
	require.Equal(t, "legacy-a", record[string(attr.ToolsetMCPSlugKey)])
	require.Equal(t, hit.ToolsetID.UUID.String(), record[string(attr.ToolsetIDKey)])
	require.Equal(t, hit.ProjectID.String(), record[string(attr.ProjectIDKey)])
	require.Equal(t, "custom_domain", record[string(attr.McpFallbackRequestHostKey)])
	require.NotContains(t, record, string(attr.McpServerIDKey))
}
