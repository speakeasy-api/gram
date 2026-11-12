package otel

import (
	"errors"
	"fmt"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// markTestRequest puts the test marker on every resource of a built request,
// so backends that tag by resource see it. It refuses a log record or span
// without both test marks, and a metric data point without the marker or with
// an ID, since an ID would start a new time series every test.
func markTestRequest(message proto.Message, testEventID string) error {
	switch request := message.(type) {
	case *collectorlogsv1.ExportLogsServiceRequest:
		for _, resourceLogs := range request.GetResourceLogs() {
			resourceLogs.Resource = markTestResource(resourceLogs.GetResource())
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				for _, record := range scopeLogs.GetLogRecords() {
					if !hasTestMarks(record.GetAttributes(), testEventID) {
						return errors.New("a test log record lacks the test marks")
					}
				}
			}
		}
	case *collectortracev1.ExportTraceServiceRequest:
		for _, resourceSpans := range request.GetResourceSpans() {
			resourceSpans.Resource = markTestResource(resourceSpans.GetResource())
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					if !hasTestMarks(span.GetAttributes(), testEventID) {
						return errors.New("a test span lacks the test marks")
					}
				}
			}
		}
	case *collectormetricsv1.ExportMetricsServiceRequest:
		for _, resourceMetrics := range request.GetResourceMetrics() {
			resourceMetrics.Resource = markTestResource(resourceMetrics.GetResource())
			for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
				for _, item := range scopeMetrics.GetMetrics() {
					for _, attributes := range metricDataPointAttributes(item) {
						if !hasTestMarker(attributes) || hasTestEventID(attributes) {
							return errors.New("a test metric data point must carry the test marker and no test event ID")
						}
					}
				}
			}
		}
	default:
		return fmt.Errorf("unsupported test request type %T", message)
	}
	return nil
}

func markTestResource(resource *resourcev1.Resource) *resourcev1.Resource {
	if resource == nil {
		resource = &resourcev1.Resource{Attributes: nil, DroppedAttributesCount: 0, EntityRefs: nil}
	}
	for _, kv := range resource.GetAttributes() {
		if kv.GetKey() == string(attr.TestEventKey) {
			kv.Value = otlpTestMarker().GetValue()
			return resource
		}
	}
	resource.Attributes = append(resource.Attributes, otlpTestMarker())
	return resource
}

func hasTestMarks(attributes []*commonv1.KeyValue, testEventID string) bool {
	if !hasTestMarker(attributes) {
		return false
	}
	for _, kv := range attributes {
		if kv.GetKey() == string(attr.TestEventIDKey) {
			return kv.GetValue().GetStringValue() == testEventID
		}
	}
	return false
}

func hasTestMarker(attributes []*commonv1.KeyValue) bool {
	for _, kv := range attributes {
		if kv.GetKey() == string(attr.TestEventKey) {
			return kv.GetValue().GetBoolValue()
		}
	}
	return false
}

func hasTestEventID(attributes []*commonv1.KeyValue) bool {
	for _, kv := range attributes {
		if kv.GetKey() == string(attr.TestEventIDKey) {
			return true
		}
	}
	return false
}

// metricDataPointAttributes returns the attributes of every data point in a metric.
func metricDataPointAttributes(item *metricsv1.Metric) [][]*commonv1.KeyValue {
	var out [][]*commonv1.KeyValue
	switch data := item.GetData().(type) {
	case *metricsv1.Metric_Gauge:
		for _, point := range data.Gauge.GetDataPoints() {
			out = append(out, point.GetAttributes())
		}
	case *metricsv1.Metric_Sum:
		for _, point := range data.Sum.GetDataPoints() {
			out = append(out, point.GetAttributes())
		}
	case *metricsv1.Metric_Histogram:
		for _, point := range data.Histogram.GetDataPoints() {
			out = append(out, point.GetAttributes())
		}
	case *metricsv1.Metric_ExponentialHistogram:
		for _, point := range data.ExponentialHistogram.GetDataPoints() {
			out = append(out, point.GetAttributes())
		}
	case *metricsv1.Metric_Summary:
		for _, point := range data.Summary.GetDataPoints() {
			out = append(out, point.GetAttributes())
		}
	}
	return out
}
