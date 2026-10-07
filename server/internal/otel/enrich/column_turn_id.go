package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnTurnID fills turn_id: the turn within the session, when the producer
// states one. Every classified event type is asked, because a turn is a
// property of the session's timeline rather than of one kind of event. It is
// populated unevenly across producers: Claude Code states a prompt id on
// each event, Codex states no turn at all, so for Codex every classified
// record counts as missing here. That count is the honest reading of the
// gap, not noise.
func columnTurnID() columnDefinition {
	return column[string]{
		key:    TurnIDColumnKey,
		byType: everyClassifiedType(getter[string]{log: dialect.LogDialect.TurnID, span: dialect.SpanDialect.TurnID}),
	}
}
