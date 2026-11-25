package openrouter

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func spanAttributes(t *testing.T, recorder *tracetest.SpanRecorder) map[attribute.Key]attribute.Value {
	t.Helper()
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	out := map[attribute.Key]attribute.Value{}
	for _, kv := range spans[0].Attributes() {
		out[kv.Key] = kv.Value
	}
	return out
}

// A streamed completion's span carries what its latency and cost depend on:
// the upstream provider, the wait for the first chunk, and the reasoning and
// cached token counts from the final usage frame.
func TestStreamingReader_RecordsProviderFirstChunkAndUsageOnSpan(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(t.Context(), "completion")

	reader := &streamingResponseReader{
		ctx:                  ctx,
		body:                 io.NopCloser(strings.NewReader("")),
		logger:               testenv.NewLogger(t),
		messageContent:       &strings.Builder{},
		lineBuf:              &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
		startTime:            time.Now(),
		requestStartedAt:     time.Now().Add(-1500 * time.Millisecond),
	}
	reader.parseSSEChunks([]byte(
		"data: {\"provider\":\"Amazon Bedrock\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120," +
			"\"prompt_tokens_details\":{\"cached_tokens\":80},\"completion_tokens_details\":{\"reasoning_tokens\":12}}}\n" +
			"data: [DONE]\n",
	))
	require.NoError(t, reader.Close())
	span.End()

	got := spanAttributes(t, recorder)
	require.Equal(t, "Amazon Bedrock", got[attr.OpenRouterUpstreamProviderKey].AsString())
	require.GreaterOrEqual(t, got[attr.OpenRouterTimeToFirstChunkKey].AsInt64(), int64(1500))
	require.Equal(t, int64(12), got[attr.GenAIUsageReasoningTokensKey].AsInt64())
	require.Equal(t, int64(80), got[attr.GenAIUsageCacheReadInputTokensKey].AsInt64())
}

// A stream that ends before any data chunk records no first-chunk time and no
// token details, rather than zeros that read as an instant, uncached call.
func TestStreamingReader_EmptyStreamRecordsNothing(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(t.Context(), "completion")

	reader := &streamingResponseReader{
		ctx:                  ctx,
		body:                 io.NopCloser(strings.NewReader("")),
		logger:               testenv.NewLogger(t),
		messageContent:       &strings.Builder{},
		lineBuf:              &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		usage:                Usage{},
		startTime:            time.Now(),
		requestStartedAt:     time.Now(),
	}
	require.NoError(t, reader.Close())
	span.End()

	got := spanAttributes(t, recorder)
	for _, key := range []attribute.Key{
		attr.OpenRouterUpstreamProviderKey,
		attr.OpenRouterTimeToFirstChunkKey,
		attr.GenAIUsageReasoningTokensKey,
		attr.GenAIUsageCacheReadInputTokensKey,
	} {
		_, ok := got[key]
		require.False(t, ok, "unexpected %s", key)
	}
}

func TestRecordCompletionSpanAttributes_NoSpanIsANoop(t *testing.T) {
	t.Parallel()
	recordCompletionSpanAttributes(context.Background(), "Anthropic", Usage{}, time.Second)
}
