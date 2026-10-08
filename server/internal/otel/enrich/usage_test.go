package enrich

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestUsageForClaudeCode(t *testing.T) {
	t.Parallel()

	require.Equal(t, "enrich-usage", (&logUsage{instruments: nil}).Name())

	t.Run("an api_request states its tokens and its cost in dollars", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 120),
			inboundTestIntAttribute("output_tokens", 30),
			logStringAttribute("cache_read_tokens", "5"),
			inboundTestIntAttribute("cache_creation_tokens", 7),
			inboundTestDoubleAttribute("cost_usd", 0.0125),
		)

		attrs := usage(t, in, record)
		require.Len(t, attrs, 5)
		require.Equal(t, int64(120), attrs[AgentInputTokensKey].AsInt64())
		require.Equal(t, int64(30), attrs[AgentOutputTokensKey].AsInt64())
		require.Equal(t, int64(5), attrs[AgentCacheReadTokensKey].AsInt64(), "stringified numbers still count")
		require.Equal(t, int64(7), attrs[AgentCacheWriteTokensKey].AsInt64())
		require.InDelta(t, 0.0125, attrs[AgentCostUSDKey].AsFloat64(), 1e-9)
	})

	t.Run("a cost stated in micros lands in dollars", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestIntAttribute("cost_usd_micros", 12_500),
		)

		require.InDelta(t, 0.0125, usage(t, in, record)[AgentCostUSDKey].AsFloat64(), 1e-9)
	})

	t.Run("a stated zero is written as a typed zero, not dropped", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestIntAttribute("cache_read_tokens", 0),
			inboundTestDoubleAttribute("cost_usd", 0),
		)

		attrs := usage(t, in, record)
		require.Equal(t, attribute.INT64, attrs[AgentCacheReadTokensKey].Type(), "a request that read nothing from the cache says so")
		require.Zero(t, attrs[AgentCacheReadTokensKey].AsInt64())
		require.Equal(t, attribute.FLOAT64, attrs[AgentCostUSDKey].Type(), "a stated zero cost is still stated")
		require.InDelta(t, 0, attrs[AgentCostUSDKey].AsFloat64(), 1e-12)
	})

	t.Run("a compaction and a tool_result carry no usage, whatever their attributes say", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)

		compaction := inboundTestLog(claudeCodeScopeName, "claude-code", "compaction",
			inboundTestIntAttribute("pre_tokens", 150_000),
			inboundTestIntAttribute("post_tokens", 20_000),
			inboundTestIntAttribute("input_tokens", 99),
		)
		require.Empty(t, usage(t, in, compaction), "a compaction's token counts are housekeeping, not a request's usage")

		result := inboundTestLog(claudeCodeScopeName, "claude-code", "tool_result",
			logStringAttribute("tool_name", "Bash"),
			inboundTestIntAttribute("input_tokens", 99),
			inboundTestDoubleAttribute("cost_usd", 1),
		)
		require.Empty(t, usage(t, in, result))

		for _, name := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"} {
			require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn(name)), name)
		}
	})
}

func TestUsageForCodex(t *testing.T) {
	t.Parallel()

	t.Run("a response.completed lands as disjoint input and cache-read tokens", func(t *testing.T) {
		t.Parallel()
		reader, meterProvider := readableMeter(t)
		in := NewInstruments(testenv.NewLogger(t), meterProvider)
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			logStringAttribute("input_token_count", "100"),
			inboundTestIntAttribute("cached_token_count", 30),
			inboundTestIntAttribute("output_token_count", 7),
		)

		attrs := usage(t, in, record)
		require.Equal(t, int64(70), attrs[AgentInputTokensKey].AsInt64(), "input excludes cache reads")
		require.Equal(t, int64(30), attrs[AgentCacheReadTokensKey].AsInt64())
		require.Equal(t, int64(7), attrs[AgentOutputTokensKey].AsInt64())
		require.NotContains(t, attrs, AgentCacheWriteTokensKey, "Codex reports no cache writes")
		require.NotContains(t, attrs, AgentCostUSDKey, "Codex reports no cost")
		require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("cache_write_tokens")))
		require.Equal(t, int64(1), counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("cost_usd")))
		require.Zero(t, counterValue(t, reader, meterAgentAttributeMissing, attr.AgentEventColumn("input_tokens")))
	})

	t.Run("a cached count larger than the input is clamped so bad data never increases usage", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			inboundTestIntAttribute("input_token_count", 10),
			inboundTestIntAttribute("cached_token_count", 50),
		)

		attrs := usage(t, in, record)
		require.Contains(t, attrs, AgentInputTokensKey, "a clamped input is written as a stated zero, not left out")
		require.Equal(t, attribute.INT64, attrs[AgentInputTokensKey].Type())
		require.Zero(t, attrs[AgentInputTokensKey].AsInt64())
		require.Equal(t, int64(10), attrs[AgentCacheReadTokensKey].AsInt64())
	})
}

func TestUsageForSemconv(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
		logStringAttribute("gen_ai.operation.name", "chat"),
		logStringAttribute("gen_ai.usage.input_tokens", "200"),
		logStringAttribute("gen_ai.usage.output_tokens", "50"),
		logStringAttribute("gen_ai.usage.cost", "0.002"),
	)

	attrs := usage(t, in, record)
	require.Equal(t, int64(200), attrs[AgentInputTokensKey].AsInt64())
	require.Equal(t, int64(50), attrs[AgentOutputTokensKey].AsInt64())
	require.InDelta(t, 0.002, attrs[AgentCostUSDKey].AsFloat64(), 1e-9)
}

func TestSpanUsageReadsAChatSpan(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	require.Equal(t, "enrich-usage", (&spanUsage{instruments: in}).Name())

	chat := inboundTestSpan("litellm", "litellm", "chat gpt-4o", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "chat"),
		spanStringAttribute("gen_ai.usage.input_tokens", "200"),
		spanStringAttribute("gen_ai.usage.output_tokens", "50"),
		spanStringAttribute("gen_ai.usage.cost", "0.002"),
	)
	attrs := enrichedSpan(t, &spanUsage{instruments: in}, chat)
	require.Equal(t, int64(200), attrs[AgentInputTokensKey].AsInt64())
	require.Equal(t, int64(50), attrs[AgentOutputTokensKey].AsInt64())
	require.InDelta(t, 0.002, attrs[AgentCostUSDKey].AsFloat64(), 1e-9)

	tool := inboundTestSpan("my-agent", "my-agent", "execute_tool search", otelv1.InboundSpan_STATUS_CODE_OK,
		spanStringAttribute("gen_ai.operation.name", "execute_tool"),
		spanStringAttribute("gen_ai.usage.input_tokens", "200"),
	)
	require.Empty(t, enrichedSpan(t, &spanUsage{instruments: in}, tool), "a tool call carries no usage")
}
