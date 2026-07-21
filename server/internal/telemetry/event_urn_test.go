package telemetry

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"
)

func TestNewEventURN_BuildsCanonicalIdentity(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"urn:gram:telemetry:event:gram_gateway:log:tool_call",
		NewEventURN(EventOriginGramGateway, EventKindLog, "tool_call"),
	)
}

func TestNewEventURN_SanitizesTypeSegment(t *testing.T) {
	t.Parallel()

	// Colons and whitespace would break the colon-delimited segment
	// structure; case and dots are preserved so producer names stay
	// recognizable (PreToolUse, codex.sse_event).
	require.Equal(t,
		"urn:gram:telemetry:event:agent_hook:log:some_weird_event",
		NewEventURN(EventOriginAgentHook, EventKindLog, "some:weird event"),
	)
	require.Equal(t,
		"urn:gram:telemetry:event:provider_otel:log:codex.sse_event",
		NewEventURN(EventOriginProviderOTEL, EventKindLog, "codex.sse_event"),
	)
	require.Equal(t,
		"urn:gram:telemetry:event:agent_hook:log:unknown",
		NewEventURN(EventOriginAgentHook, EventKindLog, "  "),
	)
}

func TestDeriveEventURN_ClassifiesKnownProducers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		legacyURN string
		attrs     map[attr.Key]any
		want      string
	}{
		{
			name:      "it classifies gateway tool calls",
			legacyURN: "tools:http:petstore:list_pets",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceToolCall)},
			want:      "urn:gram:telemetry:event:gram_gateway:log:tool_call",
		},
		{
			name:      "it classifies gateway resource reads",
			legacyURN: "tools:http:petstore:read_docs",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceResourceRead)},
			want:      "urn:gram:telemetry:event:gram_gateway:log:resource_read",
		},
		{
			name:      "it classifies gram chat completions",
			legacyURN: "chat:completion",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceChatCompletion)},
			want:      "urn:gram:telemetry:event:gram_runtime:log:chat_completion",
		},
		{
			name:      "it classifies assistant rows",
			legacyURN: "assistants:pipeline",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceAssistant)},
			want:      "urn:gram:telemetry:event:gram_runtime:log:assistant",
		},
		{
			name:      "it classifies trigger deliveries",
			legacyURN: "urn:uuid:0d5f1c1a-0000-0000-0000-000000000000",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceTrigger)},
			want:      "urn:gram:telemetry:event:gram_runtime:log:trigger",
		},
		{
			name:      "it classifies evaluations by evaluation name",
			legacyURN: "chat:resolution",
			attrs: map[attr.Key]any{
				attr.EventSourceKey:         string(EventSourceEvaluation),
				attr.GenAIEvaluationNameKey: "chat_resolution",
			},
			want: "urn:gram:telemetry:event:gram_worker:log:chat_resolution",
		},
		{
			name:      "it falls back to the event source for unnamed evaluations",
			legacyURN: "chat:resolution",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceEvaluation)},
			want:      "urn:gram:telemetry:event:gram_worker:log:evaluation",
		},
		{
			name:      "it classifies polled admin API usage as provider_api metrics",
			legacyURN: "cursor:usage:metrics",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceAPI)},
			want:      "urn:gram:telemetry:event:provider_api:metric:usage",
		},
		{
			name:      "it classifies claude OTEL logs by event name",
			legacyURN: "claude-code:otel:logs",
			attrs: map[attr.Key]any{
				attr.EventSourceKey: string(EventSourceHook),
				rawEventNameKey:     "api_request",
			},
			want: "urn:gram:telemetry:event:provider_otel:log:api_request",
		},
		{
			name:      "it classifies older claude CLIs by the body event name",
			legacyURN: "claude-code:otel:logs",
			attrs: map[attr.Key]any{
				attr.EventSourceKey: string(EventSourceHook),
				attr.LogBodyKey:     "claude_code.tool_result",
			},
			want: "urn:gram:telemetry:event:provider_otel:log:tool_result",
		},
		{
			name:      "it marks claude OTEL logs without any event name as unknown",
			legacyURN: "claude-code:otel:logs",
			attrs: map[attr.Key]any{
				attr.EventSourceKey: string(EventSourceHook),
				attr.LogBodyKey:     "api request",
			},
			want: "urn:gram:telemetry:event:provider_otel:log:unknown",
		},
		{
			name:      "it classifies claude usage rows as provider_otel metrics",
			legacyURN: "claude-code:usage:metrics",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceHook)},
			want:      "urn:gram:telemetry:event:provider_otel:metric:usage",
		},
		{
			name:      "it classifies codex usage rows as provider_otel metrics",
			legacyURN: "codex:usage:metrics",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceHook)},
			want:      "urn:gram:telemetry:event:provider_otel:metric:usage",
		},
		{
			name:      "it classifies cursor hook usage rows as agent_hook metrics",
			legacyURN: "cursor:usage:metrics",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceHook)},
			want:      "urn:gram:telemetry:event:agent_hook:metric:usage",
		},
		{
			name:      "it classifies plugin hook events by hook event name",
			legacyURN: "",
			attrs: map[attr.Key]any{
				attr.EventSourceKey: string(EventSourceHook),
				attr.HookEventKey:   "PreToolUse",
			},
			want: "urn:gram:telemetry:event:agent_hook:log:PreToolUse",
		},
		{
			name:      "it preserves gram-specific hook classifications",
			legacyURN: "",
			attrs: map[attr.Key]any{
				attr.EventSourceKey: string(EventSourceHook),
				attr.HookEventKey:   "skill.activated",
			},
			want: "urn:gram:telemetry:event:agent_hook:log:skill.activated",
		},
		{
			name:      "it marks hook rows without an event name as unknown",
			legacyURN: "",
			attrs:     map[attr.Key]any{attr.EventSourceKey: string(EventSourceHook)},
			want:      "urn:gram:telemetry:event:agent_hook:log:unknown",
		},
		{
			name:      "it marks rows with no event source as unknown",
			legacyURN: "tools:http:petstore:list_pets",
			attrs:     map[attr.Key]any{},
			want:      "urn:gram:telemetry:event:unknown:log:unknown",
		},
		{
			name:      "it keeps unrecognized event sources visible in the type segment",
			legacyURN: "",
			attrs:     map[attr.Key]any{attr.EventSourceKey: "someday_source"},
			want:      "urn:gram:telemetry:event:unknown:log:someday_source",
		},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, deriveEventURN(tt.legacyURN, tt.attrs), tt.name)
	}
}
