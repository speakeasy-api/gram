package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type ClaudeCodeLog struct{}

func (ClaudeCodeLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == "com.anthropic.claude_code.tracing"
}

func (ClaudeCodeLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	key, value := getOneLogAttr(record, claudeCodeUserPromptKey)
	if key == "" || value == "" {
		return "", nil, nil
	}

	return key, genaiconv.InputMessages{{
		Role: genaiconv.RoleUser,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: value,
		}},
		Name: nil,
	}}, nil
}

func (ClaudeCodeLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

func (ClaudeCodeLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "session.id")
	return key, value, nil
}

func (ClaudeCodeLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, userEmailKey)
	return key, value, nil
}

func (ClaudeCodeLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, vendorUserAccountIDKey)
	return key, value, nil
}

func (ClaudeCodeLog) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "gen_ai.response.id")
	return key, value, nil
}
