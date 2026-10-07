package enrich

import (
	"maps"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

// usage runs the five usage column enrichers over one record, as the
// transform does, and indexes what they wrote by key.
func usage(t *testing.T, in *Instruments, record *otelv1.InboundLogRecord) map[attribute.Key]attribute.Value {
	t.Helper()
	columns := map[attribute.Key]attribute.Value{}
	for _, enricher := range []LogEnricher{
		columnInputTokens(in), columnOutputTokens(in), columnCacheReadTokens(in), columnCacheWriteTokens(in), columnCostUSD(in),
	} {
		maps.Copy(columns, enrichedColumns(t, enricher, record))
	}
	return columns
}

func TestUsageColumnsForClaudeCode(t *testing.T) {
	t.Parallel()

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

		columns := usage(t, in, record)
		require.Len(t, columns, 5)
		require.Equal(t, int64(120), columns[InputTokensColumnKey].AsInt64())
		require.Equal(t, int64(30), columns[OutputTokensColumnKey].AsInt64())
		require.Equal(t, int64(5), columns[CacheReadTokensColumnKey].AsInt64(), "stringified numbers still count")
		require.Equal(t, int64(7), columns[CacheWriteTokensColumnKey].AsInt64())
		require.InDelta(t, 0.0125, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
	})

	t.Run("a cost stated in micros lands in dollars", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestIntAttribute("cost_usd_micros", 12_500),
		)

		require.InDelta(t, 0.0125, usage(t, in, record)[CostUSDColumnKey].AsFloat64(), 1e-9)
	})

	t.Run("a cost stated as zero is written as zero, not dropped", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(claudeCodeScopeName, "claude-code", "api_request",
			inboundTestIntAttribute("input_tokens", 1),
			inboundTestDoubleAttribute("cost_usd", 0),
		)

		columns := usage(t, in, record)
		require.Contains(t, columns, CostUSDColumnKey, "a stated zero is still stated")
		require.Equal(t, attribute.FLOAT64, columns[CostUSDColumnKey].Type())
		require.InDelta(t, 0, columns[CostUSDColumnKey].AsFloat64(), 1e-12)
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

		for _, column := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"} {
			require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn(column)), column)
		}
	})
}

func TestUsageColumnsForCodex(t *testing.T) {
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

		columns := usage(t, in, record)
		require.Equal(t, int64(70), columns[InputTokensColumnKey].AsInt64(), "input excludes cache reads")
		require.Equal(t, int64(30), columns[CacheReadTokensColumnKey].AsInt64())
		require.Equal(t, int64(7), columns[OutputTokensColumnKey].AsInt64())
		require.NotContains(t, columns, CacheWriteTokensColumnKey, "Codex reports no cache writes")
		require.NotContains(t, columns, CostUSDColumnKey, "Codex reports no cost")
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("cache_write_tokens")))
		require.Equal(t, int64(1), counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("cost_usd")))
		require.Zero(t, counterValue(t, reader, meterColumnEnricherMissing, attr.AgentEventColumn("input_tokens")))
	})

	t.Run("a cached count larger than the input is clamped so bad data never increases usage", func(t *testing.T) {
		t.Parallel()
		in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
		record := inboundTestLog(codexScopeName, "codex", "codex.sse_event",
			logStringAttribute("event.kind", "response.completed"),
			inboundTestIntAttribute("input_token_count", 10),
			inboundTestIntAttribute("cached_token_count", 50),
		)

		columns := usage(t, in, record)
		require.Zero(t, columns[InputTokensColumnKey].AsInt64())
		require.Equal(t, int64(10), columns[CacheReadTokensColumnKey].AsInt64())
	})
}

func TestUsageColumnsForSemconv(t *testing.T) {
	t.Parallel()

	in := NewInstruments(testenv.NewLogger(t), testenv.NewMeterProvider(t))
	record := inboundTestLog("litellm", "litellm", "gen_ai.client.inference.operation.details",
		logStringAttribute("gen_ai.operation.name", "chat"),
		logStringAttribute("gen_ai.usage.input_tokens", "200"),
		logStringAttribute("gen_ai.usage.output_tokens", "50"),
		logStringAttribute("gen_ai.usage.cost", "0.002"),
	)

	columns := usage(t, in, record)
	require.Equal(t, int64(200), columns[InputTokensColumnKey].AsInt64())
	require.Equal(t, int64(50), columns[OutputTokensColumnKey].AsInt64())
	require.InDelta(t, 0.002, columns[CostUSDColumnKey].AsFloat64(), 1e-9)
}
