package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnSessionID fills session_id: the agent session the record belongs to,
// as the producer states it. Every classified event type carries one, since
// rows sharing a session id are the same session by definition and there is
// no kind of event that happens outside a session. Empty, and counted, when
// the producer states none.
func columnSessionID() columnDefinition {
	return column[string]{
		key:    SessionIDColumnKey,
		byType: everyClassifiedType(question[string]{log: dialect.LogDialect.SessionID, span: dialect.SpanDialect.SessionID}),
	}
}
