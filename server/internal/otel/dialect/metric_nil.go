package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

type NilMetric struct{}

func (NilMetric) AppliesTo(*otelv1.InboundMetric) bool { return false }

func (NilMetric) SessionID(MetricDataPoint) (string, string, error) {
	return "", "", nil
}

func (NilMetric) ExternalUserID(MetricDataPoint) (string, string, error) {
	return "", "", nil
}

func (NilMetric) ExternalUserEmail(MetricDataPoint) (string, string, error) {
	return "", "", nil
}

func (NilMetric) ResponseID(MetricDataPoint) (string, string, error) {
	return "", "", nil
}
