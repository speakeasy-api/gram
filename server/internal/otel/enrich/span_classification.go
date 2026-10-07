package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

type spanClassification struct {
	instruments *Instruments
}

func (*spanClassification) Name() string {
	return classificationEnricherName
}

func (c *spanClassification) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	eventType := stated(d.EventType(span))
	if eventType == dialect.EventTypeUnclassified {
		c.instruments.recordUnclassified(ctx, missingLabel(d.Surface(span)))
	}
	return classify(
		inboundSpanSource(span),
		stated(d.EventName(span)),
		eventType,
		inboundSpanAttributeString(span, string(attr.ProviderKey)),
		stated(d.Provider(span)),
		stated(d.Surface(span)),
	), nil
}
