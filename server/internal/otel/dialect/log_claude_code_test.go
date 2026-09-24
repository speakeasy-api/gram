package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeLog(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new("com.anthropic.claude_code.events"),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute("user_prompt", "explain this trace"),
			logDialectStringAttribute("session.id", "session-id"),
			logDialectStringAttribute("user.email", "user@example.invalid"),
			logDialectStringAttribute("user.account_id", "external-user-id"),
			logDialectStringAttribute("gen_ai.response.id", "response-id"),
		},
	}).Build()

	selected := ForLog(record)
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{ClaudeCodeLog{}, SemconvLog{}}, fallback.Candidates)

	inputKey, input, err := selected.InputContent(record)
	require.NoError(t, err)
	require.Equal(t, "user_prompt", inputKey)
	require.Equal(t, genaiconv.RoleUser, input[0].Role)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, "session.id", key)
	require.Equal(t, "session-id", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, "user.account_id", key)
	require.Equal(t, "external-user-id", value)

	key, value, err = selected.ResponseID(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.id", key)
	require.Equal(t, "response-id", value)
}

func TestClaudeCodeLogAppliesToEveryClaudeCodeScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scope   string
		applies bool
	}{
		// What a 2.1 CLI actually sends its events under.
		{scope: "com.anthropic.claude_code.events", applies: true},
		{scope: "com.anthropic.claude_code.tracing", applies: true},
		{scope: "com.anthropic.claude_code", applies: true},
		{scope: "codex_otel.log_only", applies: false},
		{scope: "com.anthropic.other", applies: false},
		// Shares the root's letters but is not under it.
		{scope: "com.anthropic.claude_code_vendor", applies: false},
		{scope: "", applies: false},
	}
	for _, tc := range cases {
		t.Run("scope "+tc.scope, func(t *testing.T) {
			t.Parallel()
			record := (&otelv1.InboundLogRecord_builder{
				Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{Name: new(tc.scope)}).Build(),
			}).Build()
			require.Equal(t, tc.applies, ClaudeCodeLog{}.AppliesTo(record))
		})
	}
}

// TestClaudeCodeLogOutcomeMessageLooksPastARedactedError: Claude Code gates the
// full error behind tool-detail logging but still sends the category, so a
// redacted error must not take error_type down with it.
func TestClaudeCodeLogOutcomeMessageLooksPastARedactedError(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new("com.anthropic.claude_code.events"),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute("event.name", "tool_result"),
			logDialectStringAttribute("error", "<REDACTED>"),
			logDialectStringAttribute("error_type", "timeout"),
		},
	}).Build()

	key, value, err := ClaudeCodeLog{}.OutcomeMessage(record)
	require.NoError(t, err)
	require.Equal(t, "error_type", key)
	require.Equal(t, "timeout", value)
}

// TestBodyRepeatsEventNameInEitherSpelling: the name and the body may each
// carry the legacy prefix or not, independently, and a repeat is a repeat
// whichever way round they fall.
func TestBodyRepeatsEventNameInEitherSpelling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body, name string
		repeats    bool
	}{
		{body: "api_request", name: "api_request", repeats: true},
		{body: "claude_code.api_request", name: "api_request", repeats: true},
		{body: "api_request", name: "claude_code.api_request", repeats: true},
		{body: "claude_code.api_request", name: "claude_code.api_request", repeats: true},
		{body: "explain this trace", name: "api_request", repeats: false},
		{body: "api_response", name: "claude_code.api_request", repeats: false},
	}
	for _, tc := range cases {
		t.Run(tc.body+" vs "+tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.repeats, BodyRepeatsEventName(tc.body, tc.name))
		})
	}
}
