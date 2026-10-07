package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// Classification writes the columns that say what a record is: event_type,
// raw_event_name, source, provider and surface. It runs for every record and
// is not a per-type table, because classification is what the tables key
// on. A missing value here is normal rather than a gap in dialect coverage,
// so nothing is counted.
//
// An unclassified record gets its raw name, its source, and provider and
// surface where the dialect recognised the producer, and no type: the key is
// left off the record, which the writer reads as the empty type.
//
// Source comes from the resource, not the dialect, and is canonicalised so
// every consumer on the topic sees the same slug the event feed stores. A
// resource without a usable service.name is "unknown". Pipeline attribution
// wins over what the dialect infers for the provider: a producer the hooks
// path already attributed carries gram.provider, and that is more specific
// than the dialect's knowledge of the scope.

const classificationEnricherName = "enrich-classification"

// classify is the one decision behind both signals, over the answers each
// dialect gives for its record.
func classify(source, rawEventName, eventType, attributedProvider, dialectProvider, surface string) []attribute.KeyValue {
	out := []attribute.KeyValue{SourceColumnKey.String(source)}
	if rawEventName != "" {
		out = append(out, RawEventNameColumnKey.String(rawEventName))
	}
	if eventType != dialect.EventTypeUnclassified {
		out = append(out, EventTypeColumnKey.String(eventType))
	}
	provider := attributedProvider
	if provider == "" {
		provider = dialectProvider
	}
	if provider != "" {
		out = append(out, ProviderColumnKey.String(provider))
	}
	if surface != "" {
		out = append(out, SurfaceColumnKey.String(surface))
	}
	return out
}

// logClassification classifies log records.
type logClassification struct{}

func (*logClassification) Name() string {
	return classificationEnricherName
}

func (*logClassification) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	return classify(
		inboundLogSource(record),
		stated(d.EventName(record)),
		stated(d.EventType(record)),
		inboundLogAttributeString(record, string(attr.ProviderKey)),
		stated(d.Provider(record)),
		stated(d.Surface(record)),
	), nil
}

// spanClassification classifies spans. A span's raw name is the span name.
type spanClassification struct{}

func (*spanClassification) Name() string {
	return classificationEnricherName
}

func (*spanClassification) Enrich(_ context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	return classify(
		inboundSpanSource(span),
		stated(d.EventName(span)),
		stated(d.EventType(span)),
		inboundSpanAttributeString(span, string(attr.ProviderKey)),
		stated(d.Provider(span)),
		stated(d.Surface(span)),
	), nil
}
