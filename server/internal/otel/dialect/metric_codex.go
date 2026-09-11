package dialect

import (
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

type CodexMetric struct{}

func (CodexMetric) AppliesTo(metric *otelv1.InboundMetric) bool {
	serviceName := getMetricResourceStringAttr(metric, "service.name")
	suffix, ok := strings.CutPrefix(serviceName, "codex")
	return ok && (suffix == "" || suffix[0] == '_' || suffix[0] == '-')
}

func (CodexMetric) SessionID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "conversation.id")
	return key, value, nil
}

func (CodexMetric) ExternalUserID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, vendorUserAccountIDKey)
	return key, value, nil
}

func (CodexMetric) ExternalUserEmail(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, userEmailKey)
	return key, value, nil
}

func (CodexMetric) ResponseID(MetricDataPoint) (string, string, error) {
	return "", "", nil
}
