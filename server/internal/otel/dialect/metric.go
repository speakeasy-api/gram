package dialect

import otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

// MetricDataPoint is any OTLP metric point carrying stream attributes.
type MetricDataPoint interface {
	GetAttributes() []*otelv1.InboundMetric_KeyValue
}

type MetricDialect interface {
	AppliesTo(metric *otelv1.InboundMetric) bool
	SessionID(point MetricDataPoint) (key string, value string, err error)
	ExternalUserID(point MetricDataPoint) (key string, value string, err error)
	ExternalUserEmail(point MetricDataPoint) (key string, value string, err error)
	ResponseID(point MetricDataPoint) (key string, value string, err error)
}

var metricDialects = []MetricDialect{
	ClaudeCodeMetric{},
	CodexMetric{},
}

func ForMetric(metric *otelv1.InboundMetric) MetricDialect {
	if metric == nil {
		return NilMetric{}
	}

	for _, candidate := range metricDialects {
		if candidate.AppliesTo(metric) {
			return MetricFallback{Candidates: []MetricDialect{candidate, SemconvMetric{}}}
		}
	}

	return SemconvMetric{}
}
