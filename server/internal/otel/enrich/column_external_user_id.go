package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnExternalUserID fills external_user_id: the user's id in the
// provider's own account system, as the producer states it. Every classified
// event type is asked, since the account is a property of the session rather
// than of one kind of event. Empty, and counted, when the producer states
// none.
func columnExternalUserID() columnDefinition {
	return column[string]{
		key:    ExternalUserIDColumnKey,
		byType: everyClassifiedType(question[string]{log: dialect.LogDialect.ExternalUserID, span: dialect.SpanDialect.ExternalUserID}),
	}
}
