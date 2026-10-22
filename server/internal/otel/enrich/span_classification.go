package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

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
