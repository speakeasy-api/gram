package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func TestForSpanSelectsClaudeCodeWithSemconvFallback(t *testing.T) {
	t.Parallel()

	scopeName := "com.anthropic.claude_code.tracing"
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{Name: &scopeName}).Build(),
	}).Build()

	selected := ForSpan(span)
	fallback, ok := selected.(Fallback)
	require.True(t, ok)
	require.Equal(t, []SpanDialect{ClaudeCodeSpan{}, SemconvSpan{}}, fallback.Candidates)
}

func TestClaudeCodeSpanReadsVendorAttributes(t *testing.T) {
	t.Parallel()

	scopeName := "com.anthropic.claude_code.tracing"
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{Name: &scopeName}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("user_prompt", "explain this trace"),
			stringAttribute("session.id", "session-id"),
			stringAttribute("user.email", "user@example.invalid"),
			stringAttribute("user.account_id", "external-user-id"),
			stringAttribute("gen_ai.response.id", "response-id"),
		},
	}).Build()

	dialect := ClaudeCodeSpan{}
	contentKey, content, err := dialect.InputContent(span)
	require.NoError(t, err)
	require.Equal(t, "user_prompt", contentKey)
	require.Equal(t, textInputMessages("explain this trace"), content)

	outputKey, output, err := dialect.OutputContent(span)
	require.NoError(t, err)
	require.Empty(t, outputKey)
	require.Nil(t, output)

	key, value, err := dialect.SessionID(span)
	require.NoError(t, err)
	require.Equal(t, "session.id", key)
	require.Equal(t, "session-id", value)

	key, value, err = dialect.ExternalUserEmail(span)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = dialect.ExternalUserID(span)
	require.NoError(t, err)
	require.Equal(t, "user.account_id", key)
	require.Equal(t, "external-user-id", value)

	key, value, err = dialect.ResponseID(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.id", key)
	require.Equal(t, "response-id", value)
}

func TestClaudeCodeSpanIgnoresEmptyPrompt(t *testing.T) {
	t.Parallel()

	key, content, err := (ClaudeCodeSpan{}).InputContent((&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{stringAttribute("user_prompt", "")},
	}).Build())

	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, content)
}
