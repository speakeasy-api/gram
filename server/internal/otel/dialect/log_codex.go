package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

const (
	codexLogScopeName       = "codex_otel.log_only"
	codexUserPromptEvent    = "codex.user_prompt"
	codexRedactedUserPrompt = "[REDACTED]"
)

type CodexLog struct{}

func (CodexLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == codexLogScopeName
}

func (CodexLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	switch record.GetEventName() {
	case codexUserPromptEvent:
		key, prompt := getOneLogAttr(record, "prompt")
		if key == "" || prompt == "" || prompt == codexRedactedUserPrompt {
			return "", nil, nil
		}

		return key, genaiconv.InputMessages{{
			Role: genaiconv.RoleUser,
			Parts: []genaiconv.Part{&genaiconv.TextPart{
				Type:    genaiconv.PartTypeText,
				Content: prompt,
			}},
			Name: nil,
		}}, nil
	default:
		return "", nil, nil
	}
}

func (CodexLog) OutputContent(*otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return "", nil, nil
}

func (CodexLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "conversation.id")
	return key, value, nil
}

func (CodexLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "user.email")
	return key, value, nil
}

func (CodexLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, "user.account_id")
	return key, value, nil
}

func (CodexLog) ResponseID(*otelv1.InboundLogRecord) (string, string, error) {
	return "", "", nil
}
