package dialect

import (
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
)

func TestForSpanSelectsNilDialect(t *testing.T) {
	t.Parallel()

	require.IsType(t, NilSpan{}, ForSpan(nil))
}

func TestForSpanSelectsSemconvDialect(t *testing.T) {
	t.Parallel()

	require.IsType(t, SemconvSpan{}, ForSpan((&otelv1.InboundSpan_builder{}).Build()))
}

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
	contentKey, content, err := dialect.Content(span)
	require.NoError(t, err)
	require.Equal(t, "user_prompt", contentKey)
	require.Equal(t, []string{"explain this trace"}, content)

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

	key, content, err := (ClaudeCodeSpan{}).Content((&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{stringAttribute("user_prompt", "")},
	}).Build())

	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, content)
}

func TestSemconvSpanReadsStandardAttributes(t *testing.T) {
	t.Parallel()

	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.input.messages", "prompt"),
			stringAttribute("gen_ai.conversation.id", "conversation-id"),
			stringAttribute("user.email", "user@example.invalid"),
			stringAttribute("user.id", "user-id"),
			stringAttribute("gen_ai.response.id", "response-id"),
		},
	}).Build()

	dialect := SemconvSpan{}
	contentKey, content, err := dialect.Content(span)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.input.messages", contentKey)
	require.Equal(t, []string{"prompt"}, content)

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

func TestGetOneAttrSkipsWrongValueTypeAndUsesKeyPriority(t *testing.T) {
	t.Parallel()

	wrongTypeKey := "preferred"
	wrongTypeValue := int64(42)
	span := (&otelv1.InboundSpan_builder{
		Attributes: []*otelv1.InboundSpan_KeyValue{
			(&otelv1.InboundSpan_KeyValue_builder{
				Key: &wrongTypeKey,
				Value: (&otelv1.InboundSpan_AnyValue_builder{
					IntValue: &wrongTypeValue,
				}).Build(),
			}).Build(),
			stringAttribute("fallback", "fallback-value"),
			stringAttribute("preferred", "preferred-value"),
		},
	}).Build()

	key, value, err := getOneAttr(span, "preferred", "fallback")

	require.NoError(t, err)
	require.Equal(t, "preferred", key)
	require.Equal(t, "preferred-value", value)
}

func TestFallbackUsesFirstSuccessfulCandidate(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first candidate failed")
	fallback := Fallback{Candidates: []SpanDialect{
		stubSpanDialect{sessionKey: "", sessionValue: "", sessionErr: firstErr},
		stubSpanDialect{sessionKey: "session.id", sessionValue: "session-id", sessionErr: nil},
	}}

	key, value, err := fallback.SessionID((&otelv1.InboundSpan_builder{}).Build())

	require.NoError(t, err)
	require.Equal(t, "session.id", key)
	require.Equal(t, "session-id", value)
}

func TestFallbackReturnsJoinedErrorsWithoutAValue(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first candidate failed")
	secondErr := errors.New("second candidate failed")
	fallback := Fallback{Candidates: []SpanDialect{
		stubSpanDialect{sessionErr: firstErr},
		stubSpanDialect{sessionErr: secondErr},
	}}

	key, value, err := fallback.SessionID((&otelv1.InboundSpan_builder{}).Build())

	require.ErrorIs(t, err, firstErr)
	require.ErrorIs(t, err, secondErr)
	require.Empty(t, key)
	require.Empty(t, value)
}

func stringAttribute(key, value string) *otelv1.InboundSpan_KeyValue {
	return (&otelv1.InboundSpan_KeyValue_builder{
		Key: &key,
		Value: (&otelv1.InboundSpan_AnyValue_builder{
			StringValue: &value,
		}).Build(),
	}).Build()
}

type stubSpanDialect struct {
	sessionKey   string
	sessionValue string
	sessionErr   error
}

func (d stubSpanDialect) AppliesTo(*otelv1.InboundSpan) bool { return true }

func (d stubSpanDialect) Content(*otelv1.InboundSpan) (string, []string, error) {
	return "", nil, nil
}

func (d stubSpanDialect) SessionID(*otelv1.InboundSpan) (string, string, error) {
	return d.sessionKey, d.sessionValue, d.sessionErr
}

func (d stubSpanDialect) ExternalUserEmail(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (d stubSpanDialect) ExternalUserID(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}

func (d stubSpanDialect) ResponseID(*otelv1.InboundSpan) (string, string, error) {
	return "", "", nil
}
