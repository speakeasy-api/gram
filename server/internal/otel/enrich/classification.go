package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// logClassification writes the columns that say what a record is:
// event_type, raw_event_name, source, provider and surface. It runs for every
// record and is not a per-type table, because classification is what the
// tables key on. A missing value here is normal rather than a gap in dialect
// coverage, so nothing is counted.
//
// An unclassified record gets its raw name, its source, and provider and
// surface where the dialect recognised the producer, and no type: the key is
// left off the record, which the writer reads as the empty type.
type logClassification struct{}

func (*logClassification) Name() string {
	return "enrich-classification"
}

func (*logClassification) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)

	// Source comes from the resource, not the dialect, and is canonicalised
	// so every consumer on the topic sees the same slug the event feed
	// stores. A resource without a usable service.name is "unknown".
	out := []attribute.KeyValue{SourceColumnKey.String(inboundLogSource(record))}

	if name := stated(d.EventName(record)); name != "" {
		out = append(out, RawEventNameColumnKey.String(name))
	}
	if eventType := stated(d.EventType(record)); eventType != dialect.EventTypeUnclassified {
		out = append(out, EventTypeColumnKey.String(eventType))
	}

	// Pipeline attribution wins over what the dialect infers: a producer the
	// hooks path already attributed to a provider carries gram.provider, and
	// that is more specific than the dialect's knowledge of the scope.
	provider := inboundLogAttributeString(record, string(attr.ProviderKey))
	if provider == "" {
		provider = stated(d.Provider(record))
	}
	if provider != "" {
		out = append(out, ProviderColumnKey.String(provider))
	}

	if surface := stated(d.Surface(record)); surface != "" {
		out = append(out, SurfaceColumnKey.String(surface))
	}
	return out, nil
}
