package hooks

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
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
			strAttr("event.name", codexSSEEventName),
			strAttr("event.kind", codexResponseCompletedKind),
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

// TestLogs_StampsProviderOnCodexUsage drives the codex usage path end-to-end
// (Logs -> writeCodexUsageToClickHouse) and asserts the persisted usage row
// carries provider=openai.
func TestLogs_StampsProviderOnCodexUsage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	authCtx := hookAuthContext(t, ctx)
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)

	rec := tokenBearingRecord()
	rec.ObservedTimeUnixNano = new(nanoString(now))

	require.NoError(t, ti.service.Logs(ctx, codexLogsPayload(rec)))

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), codexUsageMetricsURN, now, 1)
	require.Contains(t, logs[0].Attributes, providerOpenAI)
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

func TestExtractCodexUsage_TokenBearingEvent(t *testing.T) {
	t.Parallel()

	points := extractCodexUsage(codexLogsPayload(tokenBearingRecord()))
	require.Len(t, points, 1)

	p := points[0]
	assert.Equal(t, "019e8c0e-740b-7113-b261-71fc14a82fb0", p.ConversationID)
	assert.Equal(t, "gpt-5.4-mini", p.Model)
	assert.Equal(t, "dev@example.com", p.UserEmail)
	assert.Equal(t, int64(92728), p.InputTokens)
	assert.Equal(t, int64(1441), p.OutputTokens)
	assert.Equal(t, int64(68992), p.CachedTokens)
	assert.Equal(t, int64(1170), p.ReasoningTokens)
	// tool_token_count is captured verbatim and happens to equal input+output.
	assert.Equal(t, int64(94169), p.ToolTokens)
	assert.Equal(t, p.InputTokens+p.OutputTokens, p.ToolTokens)
	assert.Equal(t, int64(1780468942284000000), p.TimestampNano)
}

func TestExtractCodexUsage_SkipsRecordsWithoutUsage(t *testing.T) {
	t.Parallel()

	// A response.completed with no token counts — Codex emits these too.
	noUsage := &gen.OTELLogRecord{
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", codexSSEEventName),
			strAttr("event.kind", codexResponseCompletedKind),
			strAttr("conversation.id", "conv-1"),
		},
	}
	// A non-completion SSE event with stray counts — must not be mistaken for usage.
	otherEvent := &gen.OTELLogRecord{
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", codexSSEEventName),
			strAttr("event.kind", "response.output_item.done"),
			strAttr("input_token_count", "10"),
		},
	}
	// A websocket event — different event.name entirely.
	wsEvent := &gen.OTELLogRecord{
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", "codex.websocket_event"),
			strAttr("input_token_count", "20"),
		},
	}

	points := extractCodexUsage(codexLogsPayload(noUsage, otherEvent, wsEvent))
	assert.Empty(t, points)
}

func TestExtractCodexUsage_OutputOnlyEventCounts(t *testing.T) {
	t.Parallel()

	// Presence of output alone is enough to treat the event as token-bearing.
	rec := &gen.OTELLogRecord{
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", codexSSEEventName),
			strAttr("event.kind", codexResponseCompletedKind),
			strAttr("output_token_count", "572"),
			strAttr("conversation.id", "conv-2"),
		},
	}
	points := extractCodexUsage(codexLogsPayload(rec))
	require.Len(t, points, 1)
	assert.Equal(t, int64(0), points[0].InputTokens)
	assert.Equal(t, int64(572), points[0].OutputTokens)
}

func TestExtractCodexUsage_MultipleTurnsSameConversation(t *testing.T) {
	t.Parallel()

	turn2 := &gen.OTELLogRecord{
		Attributes: []*gen.OTELAttribute{
			strAttr("event.name", codexSSEEventName),
			strAttr("event.kind", codexResponseCompletedKind),
			strAttr("input_token_count", "86277"),
			strAttr("output_token_count", "572"),
			strAttr("conversation.id", "019e8c0e-740b-7113-b261-71fc14a82fb0"),
		},
	}

	points := extractCodexUsage(codexLogsPayload(tokenBearingRecord(), turn2))
	require.Len(t, points, 2)
	// Both turns roll up to the same conversation for correlation.
	assert.Equal(t, points[0].ConversationID, points[1].ConversationID)
}
