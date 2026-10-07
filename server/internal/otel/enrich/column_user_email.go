package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnUserEmail fills user_email: the email the producer states for the
// person behind the session. Every classified event type is asked, since
// the person is a property of the session rather than of one kind of event.
// Empty, and counted, when the producer states none; the directory enricher
// reads the same answer to look the person up.
func columnUserEmail() columnDefinition {
	return column[string]{
		key:    UserEmailColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.ExternalUserEmail, span: dialect.SpanDialect.ExternalUserEmail}),
	}
}
