// Package oteltest holds assertions on built OTLP export requests.
package oteltest

import (
	"fmt"
	"slices"
	"strings"

	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// KeyCollision is a key that is a dotted prefix of another key on the same
// record or its resource. Destinations that nest dotted names, such as
// Datadog, cannot keep both: the shorter key's value is dropped.
type KeyCollision struct {
	Where  string
	Parent string
	Child  string
}

func (c KeyCollision) String() string {
	return fmt.Sprintf("%s: %q is a dotted prefix of %q", c.Where, c.Parent, c.Child)
}

// DottedPrefixCollisions lists every collision on each log record, span and
// metric data point in request, checking its keys together with its
// resource's keys. Parents named in exempt are skipped.
func DottedPrefixCollisions(request proto.Message, exempt map[string]string) ([]KeyCollision, error) {
	var out []KeyCollision
	check := func(where string, resource, record []*commonv1.KeyValue) {
		out = append(out, collisions(where, resource, record, exempt)...)
	}
	switch request := request.(type) {
	case *collectorlogsv1.ExportLogsServiceRequest:
		for _, resourceLogs := range request.GetResourceLogs() {
			resource := resourceLogs.GetResource().GetAttributes()
			for _, scopeLogs := range resourceLogs.GetScopeLogs() {
				for _, record := range scopeLogs.GetLogRecords() {
					check("log "+record.GetEventName(), resource, record.GetAttributes())
				}
			}
		}
	case *collectortracev1.ExportTraceServiceRequest:
		for _, resourceSpans := range request.GetResourceSpans() {
			resource := resourceSpans.GetResource().GetAttributes()
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					check("span "+span.GetName(), resource, span.GetAttributes())
				}
			}
		}
	case *collectormetricsv1.ExportMetricsServiceRequest:
		for _, resourceMetrics := range request.GetResourceMetrics() {
			resource := resourceMetrics.GetResource().GetAttributes()
			for _, scopeMetrics := range resourceMetrics.GetScopeMetrics() {
				for _, item := range scopeMetrics.GetMetrics() {
					for _, point := range metricPointAttributes(item) {
						check("metric "+item.GetName(), resource, point)
					}
				}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported request type %T", request)
	}
	return out, nil
}

func metricPointAttributes(item *metricsv1.Metric) [][]*commonv1.KeyValue {
	var out [][]*commonv1.KeyValue
	for _, point := range item.GetGauge().GetDataPoints() {
		out = append(out, point.GetAttributes())
	}
	for _, point := range item.GetSum().GetDataPoints() {
		out = append(out, point.GetAttributes())
	}
	for _, point := range item.GetHistogram().GetDataPoints() {
		out = append(out, point.GetAttributes())
	}
	for _, point := range item.GetExponentialHistogram().GetDataPoints() {
		out = append(out, point.GetAttributes())
	}
	for _, point := range item.GetSummary().GetDataPoints() {
		out = append(out, point.GetAttributes())
	}
	return out
}

func collisions(where string, resource, record []*commonv1.KeyValue, exempt map[string]string) []KeyCollision {
	side := make(map[string]string, len(resource)+len(record))
	for _, kv := range resource {
		side[kv.GetKey()] = "resource"
	}
	for _, kv := range record {
		if side[kv.GetKey()] == "resource" {
			side[kv.GetKey()] = "resource+record"
			continue
		}
		side[kv.GetKey()] = "record"
	}
	keys := make([]string, 0, len(side))
	for key := range side {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var out []KeyCollision
	for _, parent := range keys {
		if _, ok := exempt[parent]; ok {
			continue
		}
		for _, child := range keys {
			if strings.HasPrefix(child, parent+".") {
				out = append(out, KeyCollision{
					Where:  fmt.Sprintf("%s (%s %s, %s %s)", where, side[parent], parent, side[child], child),
					Parent: parent,
					Child:  child,
				})
			}
		}
	}
	return out
}
