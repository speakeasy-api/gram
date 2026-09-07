package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestCodexLogPrompt(t *testing.T) {
	t.Parallel()

	record := codexLogRecord(codexLogScopeName, codexUserPromptEvent,
		logDialectStringAttribute("prompt", "explain this trace"),
		logDialectStringAttribute("conversation.id", "conversation-id"),
		logDialectStringAttribute("user.email", "user@example.invalid"),
		logDialectStringAttribute("user.account_id", "external-user-id"),
		logDialectStringAttribute("gen_ai.response.id", "response-id"),
	)

	selected := ForLog(record)
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{CodexLog{}, SemconvLog{}}, fallback.Candidates)

	inputKey, input, err := selected.InputContent(record)
	require.NoError(t, err)
	require.Equal(t, "prompt", inputKey)
	require.Equal(t, genaiconv.InputMessages{{
		Role: genaiconv.RoleUser,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: "explain this trace",
		}},
		Name: nil,
	}}, input)

	outputKey, output, err := selected.OutputContent(record)
	require.NoError(t, err)
	require.Empty(t, outputKey)
	require.Nil(t, output)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, "conversation.id", key)
	require.Equal(t, "conversation-id", value)

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

func TestCodexLogSkipsRedactedPrompt(t *testing.T) {
	t.Parallel()

	record := codexLogRecord(codexLogScopeName, codexUserPromptEvent,
		logDialectStringAttribute("prompt", codexRedactedUserPrompt),
	)

	key, input, err := ForLog(record).InputContent(record)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, input)
}

func TestCodexLogSkipsEmptyPrompt(t *testing.T) {
	t.Parallel()

	record := codexLogRecord(codexLogScopeName, codexUserPromptEvent,
		logDialectStringAttribute("prompt", ""),
	)

	key, input, err := ForLog(record).InputContent(record)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, input)
}

func TestCodexLogEmptyIdentifiersUseSemconvFallback(t *testing.T) {
	t.Parallel()

	record := codexLogRecord(codexLogScopeName, codexUserPromptEvent,
		logDialectStringAttribute("conversation.id", ""),
		logDialectStringAttribute("gen_ai.conversation.id", "semconv-conversation-id"),
		logDialectStringAttribute("user.email", ""),
		logDialectStringAttribute("user.account_id", ""),
		logDialectStringAttribute("user.id", "semconv-user-id"),
	)

	selected := ForLog(record)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.conversation.id", key)
	require.Equal(t, "semconv-conversation-id", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Empty(t, value)

	key, value, err = selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, "user.id", key)
	require.Equal(t, "semconv-user-id", value)
}

func TestCodexLogDoesNotApplyToTraceSafeScope(t *testing.T) {
	t.Parallel()

	record := codexLogRecord("codex_otel.trace_safe", codexUserPromptEvent,
		logDialectStringAttribute("prompt", "explain this trace"),
	)

	require.IsType(t, SemconvLog{}, ForLog(record))
}

func codexLogRecord(scopeName, eventName string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	return (&otelv1.InboundLogRecord_builder{
		EventName: &eventName,
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: &scopeName,
		}).Build(),
		Attributes: attributes,
	}).Build()
}
