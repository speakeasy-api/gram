package dialect

import (
	"errors"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestFallbackReadsSemconvOutputMessages(t *testing.T) {
	t.Parallel()

	scopeName := "com.anthropic.claude_code.tracing"
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{Name: &scopeName}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			stringAttribute("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"text","content":"done"}],"finish_reason":"stop"}]`),
		},
	}).Build()

	contentKey, content, err := ForSpan(span).OutputContent(span)

	require.NoError(t, err)
	require.Equal(t, "gen_ai.output.messages", contentKey)
	require.Equal(t, textOutputMessages("done"), content)
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

type stubSpanDialect struct {
	sessionKey   string
	sessionValue string
	sessionErr   error
}

func (d stubSpanDialect) AppliesTo(*otelv1.InboundSpan) bool { return true }

func (d stubSpanDialect) InputContent(*otelv1.InboundSpan) (string, genaiconv.InputMessages, error) {
	return "", nil, nil
}

func (d stubSpanDialect) OutputContent(*otelv1.InboundSpan) (string, genaiconv.OutputMessages, error) {
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
