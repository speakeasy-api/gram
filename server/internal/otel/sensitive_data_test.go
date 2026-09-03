package otel

import (
	"testing"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/stretchr/testify/require"
)

func TestRedactSensitiveOTLPSpanContainers(t *testing.T) {
	t.Parallel()

	resourceAttributes := redactionTestAttributes()
	scopeAttributes := redactionTestAttributes()
	spanAttributes := redactionTestAttributes()
	eventAttributes := redactionTestAttributes()
	linkAttributes := redactionTestAttributes()
	span := &tracev1.Span{
		Attributes: spanAttributes,
		Events:     []*tracev1.Span_Event{{Attributes: eventAttributes}},
		Links:      []*tracev1.Span_Link{{Attributes: linkAttributes}},
	}
	scopeSpans := &tracev1.ScopeSpans{
		Scope: &commonv1.InstrumentationScope{Attributes: scopeAttributes},
		Spans: []*tracev1.Span{span},
	}
	request := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{{
			Resource:   &resourcev1.Resource{Attributes: resourceAttributes},
			ScopeSpans: []*tracev1.ScopeSpans{scopeSpans},
		}},
	}

	redactSensitiveOTLP(request)

	for _, attributes := range [][]*commonv1.KeyValue{
		resourceAttributes,
		scopeAttributes,
		spanAttributes,
		eventAttributes,
		linkAttributes,
	} {
		assertRedactionTestAttributes(t, attributes)
	}
}

func TestRedactSensitiveOTLPLogContainersAndNestedBody(t *testing.T) {
	t.Parallel()

	resourceAttributes := redactionTestAttributes()
	scopeAttributes := redactionTestAttributes()
	logAttributes := redactionTestAttributes()
	bodyContent := redactionTestAttribute("content", &commonv1.AnyValue{
		Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte("body secret")},
	})
	bodyAssistant := redactionTestAttribute("assistant", &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: "nested secret"},
	})
	bodySafe := redactionTestAttribute("model", &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: "safe-model"},
	})
	nestedObject := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_KvlistValue{
			KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{bodyAssistant, bodySafe}},
		},
	}
	bodyArray := redactionTestAttribute("response", &commonv1.AnyValue{
		Value: &commonv1.AnyValue_ArrayValue{
			ArrayValue: &commonv1.ArrayValue{Values: []*commonv1.AnyValue{nestedObject}},
		},
	})
	body := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_KvlistValue{
			KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{bodyContent, bodyArray}},
		},
	}
	record := &logsv1.LogRecord{Attributes: logAttributes, Body: body}
	scopeLogs := &logsv1.ScopeLogs{
		Scope:      &commonv1.InstrumentationScope{Attributes: scopeAttributes},
		LogRecords: []*logsv1.LogRecord{record},
	}
	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource:  &resourcev1.Resource{Attributes: resourceAttributes},
			ScopeLogs: []*logsv1.ScopeLogs{scopeLogs},
		}},
	}

	redactSensitiveOTLP(request)

	assertRedactionTestAttributes(t, resourceAttributes)
	assertRedactionTestAttributes(t, scopeAttributes)
	assertRedactionTestAttributes(t, logAttributes)
	require.Equal(t, "content", bodyContent.GetKey())
	require.Equal(t, redactedSensitiveDataValue, bodyContent.GetValue().GetStringValue())
	require.Equal(t, "assistant", bodyAssistant.GetKey())
	require.Equal(t, redactedSensitiveDataValue, bodyAssistant.GetValue().GetStringValue())
	require.Equal(t, "model", bodySafe.GetKey())
	require.Equal(t, "safe-model", bodySafe.GetValue().GetStringValue())
}
func TestRedactSensitiveOTLPScalarLogBodies(t *testing.T) {
	t.Parallel()

	scalarBody := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_StringValue{StringValue: "direct secret"},
	}
	arrayScalar := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte("array secret")},
	}
	arrayBody := &commonv1.AnyValue{
		Value: &commonv1.AnyValue_ArrayValue{
			ArrayValue: &commonv1.ArrayValue{Values: []*commonv1.AnyValue{arrayScalar}},
		},
	}
	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{ScopeLogs: []*logsv1.ScopeLogs{{LogRecords: []*logsv1.LogRecord{
			{Body: scalarBody},
			{Body: arrayBody},
		}}}}},
	}

	redactSensitiveOTLP(request)

	require.Equal(t, redactedSensitiveDataValue, scalarBody.GetStringValue())
	require.Equal(t, redactedSensitiveDataValue, arrayScalar.GetStringValue())
}

func TestRedactSensitiveOTLPMetricContainers(t *testing.T) {
	t.Parallel()

	resourceAttributes := redactionTestAttributes()
	scopeAttributes := redactionTestAttributes()
	metadata := redactionTestAttributes()
	gaugePointAttributes := redactionTestAttributes()
	gaugeExemplarAttributes := redactionTestAttributes()
	sumPointAttributes := redactionTestAttributes()
	sumExemplarAttributes := redactionTestAttributes()
	histogramPointAttributes := redactionTestAttributes()
	histogramExemplarAttributes := redactionTestAttributes()
	exponentialPointAttributes := redactionTestAttributes()
	exponentialExemplarAttributes := redactionTestAttributes()
	summaryPointAttributes := redactionTestAttributes()
	metrics := []*metricsv1.Metric{
		{
			Metadata: metadata,
			Data: &metricsv1.Metric_Gauge{Gauge: &metricsv1.Gauge{
				DataPoints: []*metricsv1.NumberDataPoint{{
					Attributes: gaugePointAttributes,
					Exemplars:  []*metricsv1.Exemplar{{FilteredAttributes: gaugeExemplarAttributes}},
				}},
			}},
		},
		{
			Data: &metricsv1.Metric_Sum{Sum: &metricsv1.Sum{
				DataPoints: []*metricsv1.NumberDataPoint{{
					Attributes: sumPointAttributes,
					Exemplars:  []*metricsv1.Exemplar{{FilteredAttributes: sumExemplarAttributes}},
				}},
			}},
		},
		{
			Data: &metricsv1.Metric_Histogram{Histogram: &metricsv1.Histogram{
				DataPoints: []*metricsv1.HistogramDataPoint{{
					Attributes: histogramPointAttributes,
					Exemplars:  []*metricsv1.Exemplar{{FilteredAttributes: histogramExemplarAttributes}},
				}},
			}},
		},
		{
			Data: &metricsv1.Metric_ExponentialHistogram{ExponentialHistogram: &metricsv1.ExponentialHistogram{
				DataPoints: []*metricsv1.ExponentialHistogramDataPoint{{
					Attributes: exponentialPointAttributes,
					Exemplars:  []*metricsv1.Exemplar{{FilteredAttributes: exponentialExemplarAttributes}},
				}},
			}},
		},
		{
			Data: &metricsv1.Metric_Summary{Summary: &metricsv1.Summary{
				DataPoints: []*metricsv1.SummaryDataPoint{{Attributes: summaryPointAttributes}},
			}},
		},
	}
	scopeMetrics := &metricsv1.ScopeMetrics{
		Scope:   &commonv1.InstrumentationScope{Attributes: scopeAttributes},
		Metrics: metrics,
	}
	request := &collectormetricsv1.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricsv1.ResourceMetrics{{
			Resource:     &resourcev1.Resource{Attributes: resourceAttributes},
			ScopeMetrics: []*metricsv1.ScopeMetrics{scopeMetrics},
		}},
	}

	redactSensitiveOTLP(request)

	for _, attributes := range [][]*commonv1.KeyValue{
		resourceAttributes,
		scopeAttributes,
		metadata,
		gaugePointAttributes,
		gaugeExemplarAttributes,
		sumPointAttributes,
		sumExemplarAttributes,
		histogramPointAttributes,
		histogramExemplarAttributes,
		exponentialPointAttributes,
		exponentialExemplarAttributes,
		summaryPointAttributes,
	} {
		assertRedactionTestAttributes(t, attributes)
	}
}

func redactionTestAttributes() []*commonv1.KeyValue {
	return []*commonv1.KeyValue{
		redactionTestAttribute("gen_ai.input.messages", &commonv1.AnyValue{
			Value: &commonv1.AnyValue_StringValue{StringValue: "content secret"},
		}),
		redactionTestAttribute("user.email", &commonv1.AnyValue{
			Value: &commonv1.AnyValue_StringValue{StringValue: "identity secret"},
		}),
		redactionTestAttribute("model", &commonv1.AnyValue{
			Value: &commonv1.AnyValue_StringValue{StringValue: "safe-model"},
		}),
	}
}

func redactionTestAttribute(key string, value *commonv1.AnyValue) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: value}
}

func assertRedactionTestAttributes(t *testing.T, attributes []*commonv1.KeyValue) {
	t.Helper()
	require.Len(t, attributes, 3)
	require.Equal(t, "gen_ai.input.messages", attributes[0].GetKey())
	require.Equal(t, redactedSensitiveDataValue, attributes[0].GetValue().GetStringValue())
	require.Equal(t, "user.email", attributes[1].GetKey())
	require.Equal(t, redactedSensitiveDataValue, attributes[1].GetValue().GetStringValue())
	require.Equal(t, "model", attributes[2].GetKey())
	require.Equal(t, "safe-model", attributes[2].GetValue().GetStringValue())
}
