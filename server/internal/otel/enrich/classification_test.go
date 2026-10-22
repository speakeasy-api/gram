package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/stretchr/testify/require"
)

func TestLogClassificationNamesWhatARecordIs(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}
	require.Equal(t, "enrich-classification", enricher.Name())

	t.Run("a Claude Code api_request", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("model", "claude-sonnet-4"))

		attrs := enriched(t, enricher, record)
		require.Len(t, attrs, 6)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "api_request", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSourceKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentProductKey].AsString())
		require.NotContains(t, attrs, AgentClientKey, "no terminal.type, so nothing says where it ran")
	})

	t.Run("a Codex response.completed is an api_request from openai on codex", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event", logStringAttribute("event.kind", "response.completed"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "codex.sse_event", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "codex", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.Equal(t, "codex", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("a semconv chat record names its provider and no surface", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
			logStringAttribute("gen_ai.operation.name", "chat"),
			logStringAttribute("gen_ai.provider.name", "openai"),
		)

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "gen_ai.client.inference.operation.details", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "litellm", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.NotContains(t, attrs, AgentSurfaceKey, "the semantic conventions do not say which agent was behind a request")
	})

	t.Run("an unclassified Claude Code record keeps its name and producer and gets no type", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "hook_registered")

		attrs := enriched(t, enricher, record)
		require.NotContains(t, attrs, AgentEventTypeKey)
		require.Equal(t, "hook_registered", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSourceKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("a record no dialect recognises gets only its source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog("com.example.app", "", "")

		attrs := enriched(t, enricher, record)
		require.Len(t, attrs, 1)
		require.Equal(t, SourceUnknown, attrs[AgentSourceKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request", logStringAttribute("gram.provider", "bedrock"))

		attrs := enriched(t, enricher, record)
		require.Equal(t, "bedrock", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString(), "the surface is still the dialect's")
	})

	t.Run("the source is canonicalised the way the event feed stores it", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")

		require.Equal(t, "claude-code", enriched(t, enricher, record)[AgentSourceKey].AsString())
	})

	t.Run("a hook row the hooks endpoint republished is typed by its hook event, on the hook source", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(dialect.HooksLogScope, "codex", "PostToolUse",
			logStringAttribute("gram.hook.source", "codex"),
			logStringAttribute("gram.tool.name", "shell"),
		)

		attrs := enriched(t, enricher, record)
		require.Equal(t, dialect.EventTypeToolCallResult, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "PostToolUse", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, "codex", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.Equal(t, "codex", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("pipeline attribution wins over what a hook source's family implies", func(t *testing.T) {
		t.Parallel()
		record := inboundTestLog(dialect.HooksLogScope, "codex", "PostToolUse",
			logStringAttribute("gram.provider", "anthropic"),
			logStringAttribute("gram.hook.source", "codex"),
		)

		require.Equal(t, "anthropic", enriched(t, enricher, record)[AgentProviderKey].AsString())
	})
}

func TestLogClassificationPlacesTheProductAndClient(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}

	tests := []struct {
		name    string
		record  *otelv1.InboundLogRecord
		product string
		client  string
	}{
		{
			name:    "it reads Claude Code in an editor terminal as the IDE",
			record:  inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", logStringAttribute("terminal.type", "vscode")),
			product: "claude-code",
			client:  "ide",
		},
		{
			name:    "it reads Claude Code in any other terminal as the CLI",
			record:  inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result", logStringAttribute("terminal.type", "iTerm.app")),
			product: "claude-code",
			client:  "cli",
		},
		{
			name:    "it places a hook source that names its client",
			record:  inboundTestLog(dialect.HooksLogScope, "claude-code-desktop", "PostToolUse", logStringAttribute("gram.hook.source", "claude-code-desktop")),
			product: "claude-code",
			client:  "desktop",
		},
		{
			name:    "it places a gateway call by the MCP client's name",
			record:  inboundTestLog(dialect.GatewayLogScope, "gram", "execute_tool", logStringAttribute("gen_ai.operation.name", "execute_tool"), logStringAttribute("gram.mcp.client.name", "cursor-vscode")),
			product: "cursor",
			client:  "ide",
		},
		{
			name:    "it leaves the client empty when claude.ai says nothing about the device",
			record:  inboundTestLog(dialect.GatewayLogScope, "gram", "execute_tool", logStringAttribute("gen_ai.operation.name", "execute_tool"), logStringAttribute("gram.mcp.client.name", "claude-ai")),
			product: "claude-chat",
			client:  "",
		},
		{
			name:    "it places a surface it does not know nowhere",
			record:  inboundTestLog(dialect.HooksLogScope, "x", "PostToolUse", logStringAttribute("gram.hook.source", "my-custom-agent-7")),
			product: "",
			client:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := enriched(t, enricher, tt.record)
			if tt.product == "" {
				require.NotContains(t, attrs, AgentProductKey, "an unplaced surface writes no product")
			} else {
				require.Equal(t, tt.product, attrs[AgentProductKey].AsString(), "product")
			}
			if tt.client == "" {
				require.NotContains(t, attrs, AgentClientKey, "an unknown client is not written")
			} else {
				require.Equal(t, tt.client, attrs[AgentClientKey].AsString(), "client")
			}
		})
	}
}

func TestClassifyLetsAStatedClientWinOverTheSurfaceName(t *testing.T) {
	t.Parallel()

	attrs := indexed(classify("claude-code", "", "", "", "", "claude-code-desktop", "ide"))
	require.Equal(t, "claude-code", attrs[AgentProductKey].AsString())
	require.Equal(t, "ide", attrs[AgentClientKey].AsString())

	attrs = indexed(classify("claude-code", "", "", "", "", "claude-code-desktop", ""))
	require.Equal(t, "desktop", attrs[AgentClientKey].AsString())
}

// A session lifecycle hook row has no type in the vocabulary, whichever
// surface sent it, while a tool hook row on the same source is typed.
func TestLogClassificationLeavesLifecycleHookRowsUntyped(t *testing.T) {
	t.Parallel()

	enricher := &logClassification{}

	attrs := enriched(t, enricher, inboundTestLog(dialect.HooksLogScope, "claude-code", "SessionStart", logStringAttribute("gram.hook.source", "claude-code")))
	require.NotContains(t, attrs, AgentEventTypeKey)
	require.Equal(t, "SessionStart", attrs[AgentRawEventNameKey].AsString())
	require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())

	attrs = enriched(t, enricher, inboundTestLog(dialect.HooksLogScope, "x", "SessionStart", logStringAttribute("gram.hook.source", "my-custom-agent-7")))
	require.NotContains(t, attrs, AgentEventTypeKey)
	require.Equal(t, "my-custom-agent-7", attrs[AgentSurfaceKey].AsString(), "the surface is whatever the endpoint resolved")

	attrs = enriched(t, enricher, inboundTestLog(dialect.HooksLogScope, "codex", "PostToolUse", logStringAttribute("gram.hook.source", "codex")))
	require.Equal(t, dialect.EventTypeToolCallResult, attrs[AgentEventTypeKey].AsString())
}

func TestSpanClassificationNamesWhatASpanIs(t *testing.T) {
	t.Parallel()

	enricher := &spanClassification{}
	require.Equal(t, "enrich-classification", enricher.Name())

	t.Run("a semconv chat span is an api_request named by the span, from its provider, with no surface", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("litellm", "LiteLLM", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gen_ai.provider.name", "openai"),
		)

		attrs := enrichedSpan(t, enricher, span)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "chat gpt-4o", attrs[AgentRawEventNameKey].AsString(), "a span's raw name is the span name")
		require.Equal(t, "litellm", attrs[AgentSourceKey].AsString())
		require.Equal(t, "openai", attrs[AgentProviderKey].AsString())
		require.NotContains(t, attrs, AgentSurfaceKey, "a proxy span does not say which agent was behind it")
	})

	t.Run("a Claude Code span names its producer", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
		)

		attrs := enrichedSpan(t, enricher, span)
		require.Equal(t, dialect.EventTypeAPIRequest, attrs[AgentEventTypeKey].AsString())
		require.Equal(t, "anthropic", attrs[AgentProviderKey].AsString())
		require.Equal(t, "claude-code", attrs[AgentSurfaceKey].AsString())
	})

	t.Run("pipeline attribution wins over what the dialect infers for the provider", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.anthropic.claude_code.tracing", "claude-code", "chat claude-sonnet-4", otelv1.InboundSpan_STATUS_CODE_OK,
			spanStringAttribute("gen_ai.operation.name", "chat"),
			spanStringAttribute("gram.provider", "bedrock"),
		)
		require.Equal(t, "bedrock", enrichedSpan(t, enricher, span)[AgentProviderKey].AsString())
	})

	t.Run("an unrecognised span keeps its name and source and gets no type", func(t *testing.T) {
		t.Parallel()
		span := inboundTestSpan("com.example.app", "", "GET /health", otelv1.InboundSpan_STATUS_CODE_OK)

		attrs := enrichedSpan(t, enricher, span)
		require.Len(t, attrs, 2)
		require.NotContains(t, attrs, AgentEventTypeKey)
		require.Equal(t, "GET /health", attrs[AgentRawEventNameKey].AsString())
		require.Equal(t, SourceUnknown, attrs[AgentSourceKey].AsString())
	})
}

func TestInboundLogSourceIsTheEventFeedSlugOrUnknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "claude-code", inboundLogSource(inboundTestLog(claudeCodeScopeName, "ClaudeCode", "api_request")))
	require.Equal(t, "my-agent", inboundLogSource(inboundTestLog("com.example.app", "My Agent", "")))
	require.Equal(t, SourceUnknown, inboundLogSource(inboundTestLog("com.example.app", "", "")))
}
