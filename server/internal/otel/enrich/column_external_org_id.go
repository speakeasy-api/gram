package enrich

import "github.com/speakeasy-api/gram/server/internal/otel/dialect"

// columnExternalOrgID fills external_org_id: the organization's id in the
// provider's own account system, as the producer states it. Every classified
// event type is asked, since the organization is a property of the session
// rather than of one kind of event. Only Claude Code states one today, so
// for Codex and semconv producers every classified record counts as
// missing here, which is the honest reading of the gap.
func columnExternalOrgID(in *Instruments) LogEnricher {
	return &logColumnEnricher[string]{
		column:      ExternalOrgIDColumnKey,
		byType:      everyClassifiedType(getter[string]{log: dialect.LogDialect.ExternalOrgID, span: dialect.SpanDialect.ExternalOrgID}),
		instruments: in,
	}
}
