package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func TestSemconvLog(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute("gen_ai.input.messages", `[{"role":"user","parts":[{"type":"text","content":"prompt"}]}]`),
			logDialectStringAttribute("gen_ai.output.messages", `[{"role":"assistant","parts":[{"type":"text","content":"done"}],"finish_reason":"stop"}]`),
			logDialectStringAttribute("gen_ai.conversation.id", "conversation-id"),
			logDialectStringAttribute("user.email", "user@example.invalid"),
			logDialectStringAttribute("user.id", "user-id"),
			logDialectStringAttribute("gen_ai.response.id", "response-id"),
		},
	}).Build()

	selected := ForLog(record)
	require.IsType(t, SemconvLog{}, selected)

	inputKey, input, err := selected.InputContent(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.input.messages", inputKey)
	require.Equal(t, genaiconv.RoleUser, input[0].Role)

	outputKey, output, err := selected.OutputContent(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.output.messages", outputKey)
	require.Equal(t, genaiconv.RoleAssistant, output[0].Role)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.conversation.id", key)
	require.Equal(t, "conversation-id", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Equal(t, "user.email", key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, "user.id", key)
	require.Equal(t, "user-id", value)

	key, value, err = selected.ResponseID(record)
	require.NoError(t, err)
	require.Equal(t, "gen_ai.response.id", key)
	require.Equal(t, "response-id", value)
}
