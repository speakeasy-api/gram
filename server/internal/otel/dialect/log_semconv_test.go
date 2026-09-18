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

// TestSemconvLogKeepsAClassifiedRecordsBody: the row builder only falls back to
// the body for records nothing classified, so a record semconv did classify
// would otherwise lose the words its producer wrote there.
func TestSemconvLogKeepsAClassifiedRecordsBody(t *testing.T) {
	t.Parallel()

	classified := withBody((&otelv1.InboundLogRecord_builder{
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute("gen_ai.operation.name", "chat"),
		},
	}).Build(), "the model said something worth keeping")

	key, text, err := SemconvLog{}.Text(classified)
	require.NoError(t, err)
	require.Equal(t, "body", key)
	require.Equal(t, "the model said something worth keeping", text)

	// A record semconv did not classify keeps nothing here. The row builder's
	// own fallback covers those, and a dialect that claimed one has already
	// decided what its words are.
	unclassified := withBody((&otelv1.InboundLogRecord_builder{}).Build(), "hello world")
	key, text, err = SemconvLog{}.Text(unclassified)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Empty(t, text)
}

// TestSemconvLogClassifiesEveryStandardOperation: each operation name the
// GenAI conventions define lands on a canonical type, so none of those rows
// falls through as unclassified.
func TestSemconvLogClassifiesEveryStandardOperation(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"chat":             EventTypeAPIRequest,
		"generate_content": EventTypeAPIRequest,
		"text_completion":  EventTypeAPIRequest,
		"embeddings":       EventTypeAPIRequest,
		"image_generation": EventTypeAPIRequest,
		"create_agent":     EventTypeAPIRequest,
		"invoke_agent":     EventTypeAPIRequest,
		"execute_tool":     EventTypeToolCall,
		"something_else":   EventTypeUnclassified,
	}
	for operation, want := range cases {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			record := (&otelv1.InboundLogRecord_builder{
				Attributes: []*otelv1.InboundLogRecord_KeyValue{
					logDialectStringAttribute("gen_ai.operation.name", operation),
				},
			}).Build()
			_, got, err := SemconvLog{}.EventType(record)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}
