package enrich

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

func logStringAttribute(key, value string) *otelv1.InboundLogRecord_KeyValue {
	return (&otelv1.InboundLogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundLogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}
