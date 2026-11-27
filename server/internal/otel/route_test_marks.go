package otel

import (
	"fmt"
	"slices"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// stampTestRequest marks a built, redacted test request. Metric data points get no ID: it would start a new time series every test.
func stampTestRequest(message proto.Message, testEventID string) error {
	marker := &commonv1.KeyValue{Key: string(attr.TestEventKey), Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}, KeyStrindex: 0}
	id := &commonv1.KeyValue{Key: string(attr.TestEventIDKey), Value: otlpStringValue(testEventID), KeyStrindex: 0}
	stampResource := func(resource *resourcev1.Resource) *resourcev1.Resource {
		if resource == nil {
			resource = &resourcev1.Resource{Attributes: nil, DroppedAttributesCount: 0, EntityRefs: nil}
		}
		resource.Attributes = setTestAttributes(resource.GetAttributes(), marker)
		return resource
	}

	switch request := message.(type) {
	case *collectorlogsv1.ExportLogsServiceRequest:
		for _, resourceLogs := range request.GetResourceLogs() {
			resourceLogs.Resource = stampResource(resourceLogs.GetResource())
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				for _, record := range scopeLogs.GetLogRecords() {
					record.Attributes = setTestAttributes(record.GetAttributes(), marker, id)
					record.Body = otlpStringValue(TestEventMessage)
				}
			}
		}
	case *collectortracev1.ExportTraceServiceRequest:
		for _, resourceSpans := range request.GetResourceSpans() {
			resourceSpans.Resource = stampResource(resourceSpans.GetResource())
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					span.Attributes = setTestAttributes(span.GetAttributes(), marker, id)
				}
			}
		}
	case *collectormetricsv1.ExportMetricsServiceRequest:
		for _, resourceMetrics := range request.GetResourceMetrics() {
			resourceMetrics.Resource = stampResource(resourceMetrics.GetResource())
			for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
				for _, item := range scopeMetrics.GetMetrics() {
					stampMetricDataPoints(item, marker)
				}
			}
		}
	default:
		return fmt.Errorf("unsupported test request type %T", message)
	}
	return nil
}

func stampMetricDataPoints(item *metricsv1.Metric, marker *commonv1.KeyValue) {
	switch data := item.GetData().(type) {
	case *metricsv1.Metric_Gauge:
		for _, point := range data.Gauge.GetDataPoints() {
			point.Attributes = setTestAttributes(point.GetAttributes(), marker)
		}
	case *metricsv1.Metric_Sum:
		for _, point := range data.Sum.GetDataPoints() {
			point.Attributes = setTestAttributes(point.GetAttributes(), marker)
		}
	case *metricsv1.Metric_Histogram:
		for _, point := range data.Histogram.GetDataPoints() {
			point.Attributes = setTestAttributes(point.GetAttributes(), marker)
		}
	case *metricsv1.Metric_ExponentialHistogram:
		for _, point := range data.ExponentialHistogram.GetDataPoints() {
			point.Attributes = setTestAttributes(point.GetAttributes(), marker)
		}
	case *metricsv1.Metric_Summary:
		for _, point := range data.Summary.GetDataPoints() {
			point.Attributes = setTestAttributes(point.GetAttributes(), marker)
		}
	}
}

// setTestAttributes replaces any test marks already in attributes with copies of marks.
func setTestAttributes(attributes []*commonv1.KeyValue, marks ...*commonv1.KeyValue) []*commonv1.KeyValue {
	attributes = slices.DeleteFunc(attributes, func(kv *commonv1.KeyValue) bool { return isTestEventMarkKey(kv.GetKey()) })
	for _, mark := range marks {
		attributes = append(attributes, proto.CloneOf(mark))
	}
	return attributes
}
