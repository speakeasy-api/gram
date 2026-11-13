package otel

import (
	"slices"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// isTestEventMarkKey reports a key only a data export test may write.
func isTestEventMarkKey(key string) bool {
	return key == string(attr.TestEventKey) || key == string(attr.TestEventIDKey)
}

// stripTestEventMarks drops the test marks from one attribute list, writing it back only when it changed.
func stripTestEventMarks[KV interface{ GetKey() string }](attributes []KV, set func([]KV)) {
	before := len(attributes)
	kept := slices.DeleteFunc(attributes, func(kv KV) bool { return isTestEventMarkKey(kv.GetKey()) })
	if len(kept) != before {
		set(kept)
	}
}

// Producers may not send the test marks in any attribute list the relays
// forward: a destination that filters tests out would silently hide their
// real data.

func stripLogTestEventMarks(record *otelv1.LogRecord) {
	stripTestEventMarks(record.GetAttributes(), record.SetAttributes)
	if resource := record.GetResource(); resource != nil {
		stripTestEventMarks(resource.GetAttributes(), resource.SetAttributes)
	}
	if scope := record.GetScope(); scope != nil {
		stripTestEventMarks(scope.GetAttributes(), scope.SetAttributes)
	}
}

func stripSpanTestEventMarks(span *otelv1.Span) {
	stripTestEventMarks(span.GetAttributes(), span.SetAttributes)
	if resource := span.GetResource(); resource != nil {
		stripTestEventMarks(resource.GetAttributes(), resource.SetAttributes)
	}
	if scope := span.GetScope(); scope != nil {
		stripTestEventMarks(scope.GetAttributes(), scope.SetAttributes)
	}
	for _, event := range span.GetEvents() {
		stripTestEventMarks(event.GetAttributes(), event.SetAttributes)
	}
	for _, link := range span.GetLinks() {
		stripTestEventMarks(link.GetAttributes(), link.SetAttributes)
	}
}

func stripMetricTestEventMarks(item *otelv1.Metric) {
	stripTestEventMarks(item.GetMetadata(), item.SetMetadata)
	if resource := item.GetResource(); resource != nil {
		stripTestEventMarks(resource.GetAttributes(), resource.SetAttributes)
	}
	if scope := item.GetScope(); scope != nil {
		stripTestEventMarks(scope.GetAttributes(), scope.SetAttributes)
	}
	stripExemplars := func(exemplars []*otelv1.Metric_Exemplar) {
		for _, exemplar := range exemplars {
			stripTestEventMarks(exemplar.GetFilteredAttributes(), exemplar.SetFilteredAttributes)
		}
	}
	for _, point := range item.GetGauge().GetDataPoints() {
		stripTestEventMarks(point.GetAttributes(), point.SetAttributes)
		stripExemplars(point.GetExemplars())
	}
	for _, point := range item.GetSum().GetDataPoints() {
		stripTestEventMarks(point.GetAttributes(), point.SetAttributes)
		stripExemplars(point.GetExemplars())
	}
	for _, point := range item.GetHistogram().GetDataPoints() {
		stripTestEventMarks(point.GetAttributes(), point.SetAttributes)
		stripExemplars(point.GetExemplars())
	}
	for _, point := range item.GetExponentialHistogram().GetDataPoints() {
		stripTestEventMarks(point.GetAttributes(), point.SetAttributes)
		stripExemplars(point.GetExemplars())
	}
	for _, point := range item.GetSummary().GetDataPoints() {
		stripTestEventMarks(point.GetAttributes(), point.SetAttributes)
	}
}
