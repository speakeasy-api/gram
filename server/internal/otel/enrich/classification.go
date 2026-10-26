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
// the aivendors placement of the surface; a client the dialect states wins.
func classify(source, rawEventName, eventType, attributedProvider, dialectProvider, surface, statedClient string) []attribute.KeyValue {
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
	product, placedClient, _ := aivendors.Place(surface)
	if product != "" {
		out = append(out, AgentProductKey.String(product))
	}
	client := statedClient
	if client == "" {
		client = string(placedClient)
	}
	if client != "" {
		out = append(out, AgentClientKey.String(client))
	}
	return out
}
