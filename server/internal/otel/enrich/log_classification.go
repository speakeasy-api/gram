package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

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
