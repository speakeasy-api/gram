package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

func getOneLogAttr(record *otelv1.InboundLogRecord, desired string) (string, string) {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
			continue
		}
		if value := kv.GetValue().GetStringValue(); value != "" {
			return desired, value
		}
	}
	return "", ""
}
