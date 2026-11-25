package enrich

import (
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// classify writes what a record is: event type, raw event name, source,
// provider and surface. An unclassified record keeps its raw name and source
// and gets no type. A provider the pipeline already
// attributed (gram.provider) wins over the dialect's. Product and client are
// the identity the dialect states.
func classify(source, rawEventName, eventType, attributedProvider, dialectProvider, surface string, identity aivendors.Identity) []attribute.KeyValue {
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
	if identity.ProductID != "" {
		out = append(out, AgentProductKey.String(identity.ProductID))
	}
	if identity.Client != aivendors.ClientUnknown {
		out = append(out, AgentClientKey.String(string(identity.Client)))
	}
	return out
}
