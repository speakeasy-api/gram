package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

func getOneAttr(span *otelv1.InboundSpan, keys ...string) (key string, value string, err error) {
	for _, desired := range keys {
		for _, kv := range span.GetAttributes() {
			if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
				continue
			}

			return desired, kv.GetValue().GetStringValue(), nil
		}
	}

	return "", "", nil
}
