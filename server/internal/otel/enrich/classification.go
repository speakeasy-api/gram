package enrich

import (
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// classify writes what a record is: event type, raw event name, source,
// provider and surface. Nothing is counted here; an unclassified record keeps
// its raw name and source and gets no type. A provider the pipeline already
// attributed (gram.provider) wins over the dialect's.
func classify(source, rawEventName, eventType, attributedProvider, dialectProvider, surface string) []attribute.KeyValue {
	out := []attribute.KeyValue{AgentSourceKey.String(source)}
	if rawEventName != "" {
		out = append(out, AgentRawEventNameKey.String(rawEventName))
	}
	if eventType != dialect.EventTypeUnclassified {
		out = append(out, AgentEventTypeKey.String(eventType))
	}
	provider := attributedProvider
	if provider == "" {
		provider = dialectProvider
	}
	if provider != "" {
		out = append(out, AgentProviderKey.String(provider))
	}
	if surface != "" {
		out = append(out, AgentSurfaceKey.String(surface))
	}
	return out
}
