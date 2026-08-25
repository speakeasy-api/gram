package dialect

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
	"github.com/stretchr/testify/require"
)

func complianceLogRecord(role, content string, attributes ...*otelv1.InboundLogRecord_KeyValue) *otelv1.InboundLogRecord {
	attrs := append([]*otelv1.InboundLogRecord_KeyValue{
		logDialectStringAttribute(ComplianceLogRoleAttr, role),
	}, attributes...)

	return (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new(ComplianceLogScopeName),
		}).Build(),
		Body:       (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &content}).Build(),
		Attributes: attrs,
	}).Build()
}

func TestComplianceLogSelectionAndExtraction(t *testing.T) {
	t.Parallel()

	record := complianceLogRecord("user", "what is our refund policy?",
		logDialectStringAttribute(ComplianceLogChatIDAttr, "chat-id"),
		logDialectStringAttribute(ComplianceLogUserEmailAttr, "user@example.invalid"),
		logDialectStringAttribute(ComplianceLogUserIDAttr, "external-user-id"),
		logDialectStringAttribute(ComplianceLogExternalMessageIDAttr, "msg-1"),
	)

	selected := ForLog(record)
	fallback, ok := selected.(LogFallback)
	require.True(t, ok)
	require.Equal(t, []LogDialect{ComplianceLog{}, SemconvLog{}}, fallback.Candidates)

	inputKey, input, err := selected.InputContent(record)
	require.NoError(t, err)
	require.Equal(t, "body", inputKey)
	require.Len(t, input, 1)
	require.Equal(t, genaiconv.RoleUser, input[0].Role)
	require.Equal(t, &genaiconv.TextPart{Type: genaiconv.PartTypeText, Content: "what is our refund policy?"}, input[0].Parts[0])

	// A user message yields no output content.
	outputKey, output, err := selected.OutputContent(record)
	require.NoError(t, err)
	require.Empty(t, outputKey)
	require.Nil(t, output)

	key, value, err := selected.SessionID(record)
	require.NoError(t, err)
	require.Equal(t, ComplianceLogChatIDAttr, key)
	require.Equal(t, "chat-id", value)

	key, value, err = selected.ExternalUserEmail(record)
	require.NoError(t, err)
	require.Equal(t, ComplianceLogUserEmailAttr, key)
	require.Equal(t, "user@example.invalid", value)

	key, value, err = selected.ExternalUserID(record)
	require.NoError(t, err)
	require.Equal(t, ComplianceLogUserIDAttr, key)
	require.Equal(t, "external-user-id", value)

	key, value, err = selected.ResponseID(record)
	require.NoError(t, err)
	require.Equal(t, ComplianceLogExternalMessageIDAttr, key)
	require.Equal(t, "msg-1", value)
}

func TestComplianceLogAssistantMessageIsOutputContent(t *testing.T) {
	t.Parallel()

	record := complianceLogRecord("assistant", "returns are accepted within 30 days")

	selected := ForLog(record)

	inputKey, input, err := selected.InputContent(record)
	require.NoError(t, err)
	require.Empty(t, inputKey)
	require.Nil(t, input)

	outputKey, output, err := selected.OutputContent(record)
	require.NoError(t, err)
	require.Equal(t, "body", outputKey)
	require.Len(t, output, 1)
	require.Equal(t, genaiconv.RoleAssistant, output[0].Role)
	require.Equal(t, &genaiconv.TextPart{Type: genaiconv.PartTypeText, Content: "returns are accepted within 30 days"}, output[0].Parts[0])
}

func TestComplianceLogIgnoresEmptyBodyAndForeignRoles(t *testing.T) {
	t.Parallel()

	empty := complianceLogRecord("user", "")
	key, input, err := ForLog(empty).InputContent(empty)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, input)

	foreign := complianceLogRecord("tool", "tool output")
	key, input, err = ForLog(foreign).InputContent(foreign)
	require.NoError(t, err)
	require.Empty(t, key)
	require.Nil(t, input)

	outputKey, output, err := ForLog(foreign).OutputContent(foreign)
	require.NoError(t, err)
	require.Empty(t, outputKey)
	require.Nil(t, output)
}

func TestComplianceLogDoesNotApplyToOtherScopes(t *testing.T) {
	t.Parallel()

	record := (&otelv1.InboundLogRecord_builder{
		Scope: (&otelv1.InboundLogRecord_InstrumentationScope_builder{
			Name: new("some.other.scope"),
		}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{
			logDialectStringAttribute(ComplianceLogRoleAttr, "user"),
		},
	}).Build()

	require.IsType(t, SemconvLog{}, ForLog(record))
}
