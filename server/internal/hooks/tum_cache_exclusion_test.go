package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/billing"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// TestTumCacheExclusion_AllSurfaces drives all three REAL observed-traffic
// ingest paths — the Claude OTEL logs endpoint, the Codex logs endpoint, and
// the Cursor afterAgentResponse hook — with cache-heavy usage, then asserts
// the tokens-under-management measure counts exactly input + output + cache
// WRITES for every surface, never cache READS. For Codex this pins the
// raw-stream cutover: usage is read straight off codex:otel:logs
// response.completed rows, whose cache-INCLUSIVE input count must be
// normalized to the disjoint shape by the attribute_metrics_summaries MV
// (input - cached), or the cache reads would inflate TUM by orders of
// magnitude.
func TestTumCacheExclusion_AllSurfaces(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chQueries := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	projectID := authCtx.ProjectID.String()

	// Pin event time just after the attribute_metrics_summaries MV ingestion
	// cutoff (2026-07-14 00:00:00 UTC; see server/clickhouse/schema.sql) so the
	// MV admits all three surfaces regardless of the wall clock. The Claude and
	// Codex rows carry an explicit TimeUnixNano, but the Cursor usage row is
	// stamped server-side (its payload has no timestamp), so drive the service
	// clock to the same instant.
	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ti.service.nowFunc = func() time.Time { return now }

	// Claude: disjoint semantics at the source — input excludes the cache
	// fields. TUM = 100 + 40 + 7000 (cache write) = 7140.
	require.NoError(t, ti.service.Logs(ctx, claudeLogsPayload(
		[]*gen.OTELResourceAttribute{resourceStrAttr("service.name", "claude-code")},
		nil,
		&gen.OTELLogRecord{
			TimeUnixNano: new(nanoString(now)),
			Body:         &gen.OTELLogBody{StringValue: new("claude_code.api_request")},
			Attributes: []*gen.OTELAttribute{
				strAttr("event.name", "api_request"),
				strAttr("session.id", "tum-claude-session"),
				strAttr("prompt.id", "tum-claude-prompt"),
				strAttr("model", "claude-4.6"),
				strAttr("input_tokens", "100"),
				strAttr("output_tokens", "40"),
				strAttr("cache_read_tokens", "50000"),
				strAttr("cache_creation_tokens", "7000"),
			},
		},
	)))

	// Codex: cache-INCLUSIVE input semantics at the source (OpenAI-style:
	// cached is a subset of input). The row persists as a raw codex:otel:logs
	// row and the MV normalizes to disjoint components: input 9000 - 8800
	// cached = 200, plus output 60. TUM = 200 + 60 = 260 (no cache writes).
	require.NoError(t, ti.service.Logs(ctx, codexLogsPayload(&gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(now)),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.sse_event"),
			strAttr("event.kind", "response.completed"),
			strAttr("conversation.id", "tum-codex-conv"),
			strAttr("model", "gpt-5.4-codex"),
			strAttr("input_token_count", "9000"),
			strAttr("cached_token_count", "8800"),
			strAttr("output_token_count", "60"),
		},
	})))

	// Cursor: disjoint semantics at the source (Anthropic-style four-way
	// split). TUM = 300 + 25 + 3000 (cache write) = 3325.
	email := "tum-cursor@example.com"
	convID := "tum-cursor-conv"
	inputTokens := 300
	outputTokens := 25
	cacheReadTokens := 40000
	cacheWriteTokens := 3000
	_, err := ti.service.Cursor(ctx, &gen.CursorPayload{
		HookEventName:    "afterAgentResponse",
		UserEmail:        &email,
		ConversationID:   &convID,
		InputTokens:      &inputTokens,
		OutputTokens:     &outputTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheWriteTokens,
	})
	require.NoError(t, err)

	dayStart := now.Truncate(24 * time.Hour)
	params := telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          []string{projectID},
		StartUnixNano:       dayStart.UnixNano(),
		EndUnixNano:         dayStart.Add(24 * time.Hour).UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	}

	// The aggregate materializes asynchronously; converge on the exact
	// cross-surface total, then pin each surface's slice. 100k+ cache tokens
	// are in flight, so any leakage overshoots by orders of magnitude.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		buckets, err := chQueries.GetTokensUnderManagementByDay(ctx, params)
		if !assert.NoError(c, err) {
			return
		}
		var total int64
		for _, b := range buckets {
			total += b.Tokens
		}
		assert.Equal(c, int64(7140+260+3325), total,
			"TUM must count input + output + cache writes, never cache reads")
	}, 15*time.Second, 200*time.Millisecond)

	rows, err := chQueries.GetTumBreakdownDimByDay(ctx, params, "hook_source")
	require.NoError(t, err)
	bySurface := map[string]int64{}
	for _, r := range rows {
		bySurface[r.Value] += r.Tokens
	}
	require.Equal(t, int64(7140), bySurface["claude-code"])
	// Codex usage comes straight off the raw codex:otel:logs row: the
	// cache-inclusive input is normalized to 200 disjoint input + 60 output.
	require.Equal(t, int64(260), bySurface["codex"])
	require.Equal(t, int64(3325), bySurface["cursor"])
}
