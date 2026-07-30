package hooks

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// strAttr builds an OTLP string-valued log attribute, the shape Codex emits.
func strAttr(key, val string) *gen.OTELAttribute {
	return &gen.OTELAttribute{Key: key, Value: &gen.OTELAttributeValue{StringValue: new(val)}}
}

// codexLogsPayload wraps records under the Codex resource (service.name).
func codexLogsPayload(records ...*gen.OTELLogRecord) *gen.LogsPayload {
	return &gen.LogsPayload{
		ResourceLogs: []*gen.OTELResourceLog{{
			Resource: &gen.OTELResource{
				Attributes: []*gen.OTELResourceAttribute{{
					Key:   "service.name",
					Value: &gen.OTELAttributeValue{StringValue: new(codexServiceName)},
				}},
			},
			ScopeLogs: []*gen.OTELScopeLog{{LogRecords: records}},
		}},
	}
}

// tokenBearingRecord mirrors a real response.completed SSE event with usage.
func tokenBearingRecord() *gen.OTELLogRecord {
	return &gen.OTELLogRecord{
		ObservedTimeUnixNano: new("1780468942284000000"),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.sse_event"),
			strAttr("event.kind", "response.completed"),
			strAttr("input_token_count", "92728"),
			strAttr("output_token_count", "1441"),
			strAttr("cached_token_count", "68992"),
			strAttr("reasoning_token_count", "1170"),
			strAttr("tool_token_count", "94169"),
			strAttr("conversation.id", "019e8c0e-740b-7113-b261-71fc14a82fb0"),
			strAttr("model", "gpt-5.4-mini"),
			strAttr("user.email", "dev@example.com"),
		},
	}
}

// codexMetricsPayload wraps metrics under the Codex resource (service.name).
func codexMetricsPayload(metrics ...*gen.OTELMetric) *gen.MetricsPayload {
	return &gen.MetricsPayload{
		ResourceMetrics: []*gen.OTELResourceMetrics{{
			Resource: &gen.OTELResource{
				Attributes: []*gen.OTELResourceAttribute{
					resourceStrAttr("service.name", codexServiceName),
				},
			},
			ScopeMetrics: []*gen.OTELScopeMetrics{{Metrics: metrics}},
		}},
	}
}

func TestLogs_PersistsCodexOTELRecords(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	conversationID := "019e8c0e-740b-7113-b261-71fc14a82fb0"
	traceID := "1af7651916cd43dd8448eb211c80319c"
	spanID := "c7ad6b7169203331"
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	observed := timestamp.Add(2 * time.Second)

	// One token-bearing completion and one tool decision: the completion also
	// feeds the usage path, but BOTH must land as raw codex:otel:logs rows —
	// that is the parity with Claude's persisted OTEL stream.
	completion := tokenBearingRecord()
	completion.TimeUnixNano = new(nanoString(timestamp))
	completion.ObservedTimeUnixNano = new(nanoString(observed))
	completion.TraceID = new(traceID)
	completion.SpanID = new(spanID)
	completion.Body = &gen.OTELLogBody{StringValue: new("codex.sse_event")}

	toolDecision := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(timestamp.Add(time.Second))),
		Body:         &gen.OTELLogBody{StringValue: new("codex.tool_decision")},
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.tool_decision"),
			strAttr("tool_name", "shell"),
			strAttr("decision", "approved"),
			strAttr("conversation.id", conversationID),
			strAttr("user.email", "dev@example.com"),
		},
	}

	require.NoError(t, ti.service.Logs(ctx, codexLogsPayload(completion, toolDecision)))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexOTELLogsURN, timestamp, 2)

	first := logs[1]
	require.Equal(t, timestamp.UnixNano(), first.TimeUnixNano)
	require.Equal(t, observed.UnixNano(), first.ObservedTimeUnixNano)
	require.Equal(t, "codex.sse_event", first.Body)
	require.NotNil(t, first.TraceID)
	require.Equal(t, traceID, *first.TraceID)
	require.NotNil(t, first.SpanID)
	require.Equal(t, spanID, *first.SpanID)
	require.NotNil(t, first.GramChatID)
	require.Equal(t, conversationID, *first.GramChatID)

	// Native codex attributes are preserved verbatim...
	require.Contains(t, first.Attributes, "input_token_count")
	require.Contains(t, first.Attributes, "92728")
	// ...and normalized onto the canonical gen_ai dimensions.
	require.Contains(t, first.Attributes, "gen_ai")
	require.Contains(t, first.Attributes, conversationID)
	require.Contains(t, first.Attributes, "gpt-5.4-mini")
	require.Contains(t, first.Attributes, providerOpenAI)

	require.Contains(t, first.ResourceAttributes, "service")
	require.Contains(t, first.ResourceAttributes, codexServiceName)

	second := logs[0]
	require.Equal(t, "codex.tool_decision", second.Body)
	require.Contains(t, second.Attributes, "tool_name")
	require.Contains(t, second.Attributes, "shell")
	require.NotNil(t, second.GramChatID)
	require.Equal(t, conversationID, *second.GramChatID)

	// Codex payloads must produce ONLY the raw stream: the deprecated derived
	// usage rows must never be written, and codex traffic must never leak into
	// Claude's raw stream.
	require.Never(t, func() bool {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      []string{"codex:usage:metrics", "claude-code:otel:logs"},
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) > 0
	}, 300*time.Millisecond, 50*time.Millisecond)
}

// TestLogs_CodexUsageRollsUpToAttributeMetrics pins the raw-stream cutover
// end to end: token-bearing codex:otel:logs response.completed rows are the
// sole Codex usage source for attribute_metrics_summaries, with the
// cache-INCLUSIVE input count normalized to the disjoint shape (input -
// cached) by the MV, and non-token Codex events contributing nothing.
func TestLogs_CodexUsageRollsUpToAttributeMetrics(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chQueries := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	// Pin event time just after the attribute_metrics_summaries MV ingestion
	// cutoff (2026-07-14 00:00:00 UTC; see server/clickhouse/schema.sql) so
	// rows are admitted regardless of the wall clock.
	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)

	// Two token-bearing turns in one conversation. The first is cache-heavy:
	// its input count includes 8800 cached tokens that must land in
	// cache_read_input_tokens, not the input/total sums. The second reports no
	// cached count (cached = 0).
	turn1 := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(now)),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.sse_event"),
			strAttr("event.kind", "response.completed"),
			strAttr("conversation.id", "conv-rollup"),
			strAttr("model", "gpt-5.4-codex"),
			strAttr("input_token_count", "9000"),
			strAttr("cached_token_count", "8800"),
			strAttr("output_token_count", "60"),
			strAttr("user.email", "codex-rollup@example.com"),
		},
	}
	turn2 := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(now.Add(time.Second))),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.sse_event"),
			strAttr("event.kind", "response.completed"),
			strAttr("conversation.id", "conv-rollup"),
			strAttr("model", "gpt-5.4-codex"),
			strAttr("input_token_count", "500"),
			strAttr("output_token_count", "40"),
			strAttr("user.email", "codex-rollup@example.com"),
		},
	}
	// Must NOT contribute usage: a completion without token counts and a tool
	// decision (tool calls only ever come from hook rows, not raw OTEL rows).
	noTokens := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(now.Add(2 * time.Second))),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.sse_event"),
			strAttr("event.kind", "response.completed"),
			strAttr("conversation.id", "conv-rollup"),
			strAttr("model", "gpt-5.4-codex"),
		},
	}
	toolDecision := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(now.Add(3 * time.Second))),
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.tool_decision"),
			strAttr("tool_name", "shell"),
			strAttr("decision", "approved"),
			strAttr("conversation.id", "conv-rollup"),
		},
	}

	require.NoError(t, ti.service.Logs(ctx, codexLogsPayload(turn1, turn2, noTokens, toolDecision)))

	params := telemetryrepo.AttributeMetricsQueryParams{
		ProjectIDs:      []string{authCtx.ProjectID.String()},
		TimeStart:       now.Add(-time.Hour).UnixNano(),
		TimeEnd:         now.Add(time.Hour).UnixNano(),
		GroupBy:         "model",
		SortBy:          "total_tokens",
		Filters:         nil,
		IntervalSeconds: 0,
	}

	// The aggregate materializes asynchronously; converge on the codex model
	// group, then pin every measure.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, err := chQueries.QueryAttributeMetricsTable(ctx, params)
		if !assert.NoError(c, err) {
			return
		}
		var codexRow *telemetryrepo.AttributeMetricsRow
		for i := range rows {
			if rows[i].GroupValue == "gpt-5.4-codex" {
				codexRow = &rows[i]
			}
		}
		if !assert.NotNil(c, codexRow, "codex model group must appear in attribute_metrics_summaries") {
			return
		}
		// Disjoint input: (9000 - 8800) + 500 = 700.
		assert.Equal(c, int64(700), codexRow.TotalInputTokens)
		assert.Equal(c, int64(100), codexRow.TotalOutputTokens)
		// total = input + output + cache writes; Codex reports no cache writes.
		assert.Equal(c, int64(800), codexRow.TotalTokens)
		assert.Equal(c, int64(8800), codexRow.CacheReadInputTokens)
		assert.Equal(c, int64(0), codexRow.CacheCreationInputTokens)
		// Codex exports no cost.
		assert.Zero(c, codexRow.TotalCost)
		assert.Equal(c, uint64(1), codexRow.TotalChats)
		// Raw OTEL tool events are not tool calls; only hook rows count.
		assert.Equal(c, uint64(0), codexRow.TotalToolCalls)
	}, 15*time.Second, 200*time.Millisecond)
}

func TestIsCodexLogsPayload(t *testing.T) {
	t.Parallel()

	assert.True(t, isCodexLogsPayload(codexLogsPayload(tokenBearingRecord())))
	assert.False(t, isCodexLogsPayload(nil))

	claude := &gen.LogsPayload{
		ResourceLogs: []*gen.OTELResourceLog{{
			Resource: &gen.OTELResource{
				Attributes: []*gen.OTELResourceAttribute{{
					Key:   "service.name",
					Value: &gen.OTELAttributeValue{StringValue: new("claude-code")},
				}},
			},
		}},
	}
	assert.False(t, isCodexLogsPayload(claude))
}

func TestLogs_AttributesCodexOTELRecordsToResolvedUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)

	userID := uuid.NewString()
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, "codex-otel@example.com")

	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	rec := &gen.OTELLogRecord{
		TimeUnixNano: new(nanoString(timestamp)),
		Body:         &gen.OTELLogBody{StringValue: new("codex.user_prompt")},
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.user_prompt"),
			strAttr("conversation.id", "conv-attribution"),
			strAttr("user.email", "codex-otel@example.com"),
		},
	}

	require.NoError(t, ti.service.Logs(ctx, codexLogsPayload(rec)))

	// User identity is carried in the attributes JSON (UserInfo.AsAttributes),
	// so assert the resolved Gram user id landed on the row.
	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexOTELLogsURN, timestamp, 1)
	require.Contains(t, logs[0].Attributes, userID)
	require.Contains(t, logs[0].Attributes, "codex-otel@example.com")
}

func TestIsCodexMetricsPayload(t *testing.T) {
	t.Parallel()

	assert.True(t, isCodexMetricsPayload(codexMetricsPayload()))
	assert.False(t, isCodexMetricsPayload(nil))

	claude := &gen.MetricsPayload{
		ResourceMetrics: []*gen.OTELResourceMetrics{{
			Resource: &gen.OTELResource{
				Attributes: []*gen.OTELResourceAttribute{
					resourceStrAttr("service.name", "claude-code"),
				},
			},
		}},
	}
	assert.False(t, isCodexMetricsPayload(claude))
}

func TestMetrics_PersistsCodexOTELMetricDataPoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	toolCalls := "codex.tool.call"
	unit := "1"
	payload := codexMetricsPayload(&gen.OTELMetric{
		Name: &toolCalls,
		Unit: &unit,
		Sum: &gen.OTELSum{
			// Codex exports cumulative counters; unlike the Claude extractor,
			// raw persistence must accept any temporality.
			AggregationTemporality: "AGGREGATION_TEMPORALITY_CUMULATIVE",
			DataPoints: []*gen.OTELNumberDataPoint{{
				TimeUnixNano: new(nanoString(timestamp)),
				AsInt:        "3",
				Attributes: []*gen.OTELAttribute{
					strAttr("tool_name", "shell"),
					strAttr("conversation.id", "conv-metrics-1"),
					strAttr("user.email", "dev@example.com"),
				},
			}},
		},
	})

	require.NoError(t, ti.service.Metrics(ctx, payload))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexOTELMetricsURN, timestamp, 1)

	row := logs[0]
	require.Equal(t, "codex.tool.call", row.Body)
	require.Equal(t, timestamp.UnixNano(), row.TimeUnixNano)
	require.NotNil(t, row.GramChatID)
	require.Equal(t, "conv-metrics-1", *row.GramChatID)
	require.Contains(t, row.Attributes, "metric")
	require.Contains(t, row.Attributes, "codex.tool.call")
	require.Contains(t, row.Attributes, "tool_name")
	require.Contains(t, row.Attributes, "shell")
	require.Contains(t, row.Attributes, providerOpenAI)
	require.Contains(t, row.ResourceAttributes, codexServiceName)
}

func TestMetrics_CodexPayloadDoesNotWriteClaudeUsageRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	timestamp := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	// A codex payload whose metric name collides with nothing Claude-shaped
	// must never be routed through the Claude usage extractor.
	sse := "codex.sse_event"
	payload := codexMetricsPayload(&gen.OTELMetric{
		Name: &sse,
		Sum: &gen.OTELSum{
			AggregationTemporality: "AGGREGATION_TEMPORALITY_DELTA",
			DataPoints: []*gen.OTELNumberDataPoint{{
				TimeUnixNano: new(nanoString(timestamp)),
				AsInt:        "1",
				Attributes: []*gen.OTELAttribute{
					strAttr("session.id", "claude-lookalike-session"),
				},
			}},
		},
	})

	require.NoError(t, ti.service.Metrics(ctx, payload))

	waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexOTELMetricsURN, timestamp, 1)

	require.Never(t, func() bool {
		logs, err := chClient.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     timestamp.Add(-time.Minute).UnixNano(),
			TimeEnd:       time.Now().Add(time.Minute).UnixNano(),
			GramURNs:      []string{"claude-code:usage:metrics"},
			SortOrder:     "desc",
			Cursor:        "",
			Limit:         10,
		})
		return err == nil && len(logs) > 0
	}, 300*time.Millisecond, 50*time.Millisecond)
}
