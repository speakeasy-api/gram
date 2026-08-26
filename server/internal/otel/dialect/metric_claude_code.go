package dialect

import (
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

type ClaudeCodeMetric struct{}

func (ClaudeCodeMetric) AppliesTo(metric *otelv1.InboundMetric) bool {
	return strings.HasPrefix(metric.GetName(), "claude_code.")
}

func (ClaudeCodeMetric) SessionID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "session.id")
	return key, value, nil
}

func (ClaudeCodeMetric) ExternalUserID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "user.account_id")
	return key, value, nil
}

func (ClaudeCodeMetric) ExternalUserEmail(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "user.email")
	return key, value, nil
}

func (ClaudeCodeMetric) ResponseID(point MetricDataPoint) (string, string, error) {
	key, value := getOneMetricPointAttr(point, "gen_ai.response.id")
	return key, value, nil
}
