package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

// Wire contract between the compliance chat mirror
// (server/internal/aiintegrations) and this dialect. The mirror publishes one
// InboundLogRecord per imported chat message using these scope and attribute
// names; AppliesTo and the extractors below read the same constants so the
// producer and interpreter cannot drift.
const (
	// ComplianceLogScopeName is the instrumentation scope name stamped on
	// records mirrored from provider compliance API chat imports.
	ComplianceLogScopeName = "com.speakeasy.gram.compliance_import"

	// ComplianceLogRoleAttr carries the chat message role. Only "user" and
	// "assistant" are produced today; the extractors ignore anything else.
	ComplianceLogRoleAttr = "gram.chat.role"

	// ComplianceLogChatIDAttr carries the Gram chat UUID the message belongs
	// to, which doubles as the record's session identity.
	ComplianceLogChatIDAttr = "gram.chat.id"

	// ComplianceLogExternalMessageIDAttr carries the provider's message or
	// event id — the same value that dedupes the Postgres import.
	ComplianceLogExternalMessageIDAttr = "gram.chat.external_message_id"

	// ComplianceLogUserEmailAttr carries the provider actor email when the
	// compliance feed reported one.
	ComplianceLogUserEmailAttr = "user.email"

	// ComplianceLogUserIDAttr carries the provider's user/account id.
	ComplianceLogUserIDAttr = "user.id"
)

// complianceLogContentKey is the key InputContent/OutputContent report for
// extracted content. The message text rides the record body rather than an
// attribute, so the key names the body instead of an attribute.
const complianceLogContentKey = "body"

// ComplianceLog interprets chat messages imported from provider compliance
// APIs and mirrored onto the inbound log topic. The message text rides the
// record body; the role attribute routes it to input (user) or output
// (assistant) content.
type ComplianceLog struct{}

func (ComplianceLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == ComplianceLogScopeName
}

func (ComplianceLog) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	content := complianceLogContent(record, genaiconv.RoleUser)
	if content == "" {
		return "", nil, nil
	}

	return complianceLogContentKey, genaiconv.InputMessages{{
		Role: genaiconv.RoleUser,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: content,
		}},
		Name: nil,
	}}, nil
}

func (ComplianceLog) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	content := complianceLogContent(record, genaiconv.RoleAssistant)
	if content == "" {
		return "", nil, nil
	}

	return complianceLogContentKey, genaiconv.OutputMessages{{
		Role: genaiconv.RoleAssistant,
		Parts: []genaiconv.Part{&genaiconv.TextPart{
			Type:    genaiconv.PartTypeText,
			Content: content,
		}},
		Name:         nil,
		FinishReason: "",
	}}, nil
}

func (ComplianceLog) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, ComplianceLogChatIDAttr)
	return key, value, nil
}

func (ComplianceLog) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, ComplianceLogUserEmailAttr)
	return key, value, nil
}

func (ComplianceLog) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, ComplianceLogUserIDAttr)
	return key, value, nil
}

func (ComplianceLog) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, ComplianceLogExternalMessageIDAttr)
	return key, value, nil
}

// complianceLogContent returns the record body when the record's role
// attribute matches want, and empty otherwise.
func complianceLogContent(record *otelv1.InboundLogRecord, want genaiconv.Role) string {
	_, role := getOneLogAttr(record, ComplianceLogRoleAttr)
	if genaiconv.Role(role) != want {
		return ""
	}
	return record.GetBody().GetStringValue()
}
