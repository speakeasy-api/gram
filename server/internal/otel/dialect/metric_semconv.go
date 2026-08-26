package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type SemconvMetric struct{}

func (SemconvMetric) AppliesTo(*otelv1.InboundMetric) bool { return true }

func (SemconvMetric) SessionID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "gen_ai.conversation.id")
	return key, value, nil
}

func (SemconvMetric) ExternalUserID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "user.id")
	return key, value, nil
}

func (SemconvMetric) ExternalUserEmail(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "user.email")
	return key, value, nil
}

func (SemconvMetric) ResponseID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "gen_ai.response.id")
	return key, value, nil
}
