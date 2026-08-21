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
			Name: new("com.anthropic.claude_code.tracing"),
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
