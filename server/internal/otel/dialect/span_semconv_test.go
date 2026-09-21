package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func TestForSpanSelectsSemconvDialect(t *testing.T) {
	t.Parallel()

	require.IsType(t, SemconvSpan{}, ForSpan((&otelv1.InboundSpan_builder{}).Build()))
}

func TestSemconvSpanReadsStandardAttributes(t *testing.T) {
	t.Parallel()

	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"prompt"}]}]`),
			stringAttribute("gen_ai.conversation.id", "conversation-id"),
			stringAttribute("user.email", "user@example.invalid"),
			stringAttribute("user.id", "user-id"),
			stringAttribute("gen_ai.response.id", "response-id"),
		},
	}).Build()

	dialect := SemconvSpan{}
	contentKey, content, err := dialect.InputContent(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.input.messages", contentKey)
	require.Equal(t, textInputMessages("prompt"), content)

	key, value, err := dialect.SessionID(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.conversation.id", key)
	require.Equal(t, "conversation-id", value)

	key, value, err = dialect.ExternalUserEmail(span)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = dialect.ExternalUserID(span)
	require.NoError(t, err)
	require.Equal(t, "user.id", key)
	require.Equal(t, "user-id", value)

	key, value, err = dialect.ResponseID(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.id", key)
	require.Equal(t, "response-id", value)
}

func TestSemconvSpanReadsStructuredInputMessages(t *testing.T) {
	t.Parallel()

	key := "gen_ai.input.messages"
	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			structuredMessagesAttribute(key, "user", "explain this trace", ""),
		},
	}).Build()

	contentKey, content, err := (SemconvSpan{}).InputContent(span)

	require.NoError(t, err)
	require.Equal(t, key, contentKey)
	require.Equal(t, textInputMessages("explain this trace"), content)
}

func TestSemconvSpanReadsStructuredOutputMessages(t *testing.T) {
	t.Parallel()

	key := "gen_ai.output.messages"
	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			structuredMessagesAttribute(key, "assistant", "completed the task", "stop"),
		},
	}).Build()

	contentKey, content, err := (SemconvSpan{}).OutputContent(span)

	require.NoError(t, err)
	require.Equal(t, key, contentKey)
	require.Equal(t, textOutputMessages("completed the task"), content)
}

func TestSemconvSpanSkipsPlainTextInputMessages(t *testing.T) {
	t.Parallel()

	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.input.messages", "plain text prompt"),
		},
	}).Build()

	contentKey, content, err := (SemconvSpan{}).InputContent(span)

	require.NoError(t, err)
	require.Empty(t, contentKey)
	require.Nil(t, content)
}

func TestSemconvSpanSkipsNonArrayOutputMessages(t *testing.T) {
	t.Parallel()

	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.output.messages", `{"role":"assistant"}`),
		},
	}).Build()

	contentKey, content, err := (SemconvSpan{}).OutputContent(span)

	require.NoError(t, err)
	require.Empty(t, contentKey)
	require.Nil(t, content)
}

// TestSemconvSpanModelSkipsAnEmptyResponseModel: an attribute that is present
// but empty states nothing, so a stated request model must still be found. The
// log accessor already skips empties; this keeps the two signals in step.
func TestSemconvSpanModelSkipsAnEmptyResponseModel(t *testing.T) {
	t.Parallel()

	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.response.model", ""),
			stringAttribute("gen_ai.request.model", "claude-opus-5"),
		},
	}).Build()

	key, value, err := SemconvSpan{}.Model(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.request.model", key)
	require.Equal(t, "claude-opus-5", value)
}

// TestSemconvSpanDurationIgnoresAMissingStart: a zero start is a start the
// producer never stated, not the epoch. Subtracting it would report the time
// since 1970 as the span's duration.
func TestSemconvSpanDurationIgnoresAMissingStart(t *testing.T) {
	t.Parallel()

	end := uint64(1_700_000_000_000_000_000)
	span := (&otelv1.InboundSpan_builder{EndTimeUnixNano: &end}).Build()

	key, duration, err := SemconvSpan{}.DurationNano(span)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Zero(t, duration)
}
