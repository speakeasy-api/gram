package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

type logClassification struct {
	instruments *Instruments
}

func (*logClassification) Name() string {
	return classificationEnricherName
}

func (c *logClassification) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType == dialect.EventTypeUnclassified {
		c.instruments.recordUnclassified(ctx, missingLabel(d.Surface(record)))
	}
	return classify(
		inboundLogSource(record),
		stated(d.EventName(record)),
		eventType,
		inboundLogAttributeString(record, string(attr.ProviderKey)),
		stated(d.Provider(record)),
		stated(d.Surface(record)),
	), nil
}
