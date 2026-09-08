package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

func getOneMetricPointAttr(point MetricDataPoint, keys ...string) (string, string) {
	if point == nil {
		return "", ""
	}

	for _, desired := range keys {
		for _, kv := range point.GetAttributes() {
			if kv.GetKey() != desired || !kv.GetValue().HasStringValue() {
				continue
			}
			if value := kv.GetValue().GetStringValue(); value != "" {
				return desired, value
			}
		}
	}

	return "", ""
}

func getMetricResourceStringAttr(metric *otelv1.InboundMetric, desired string) string {
	for _, kv := range metric.GetResource().GetAttributes() {
		if kv.GetKey() == desired && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}
