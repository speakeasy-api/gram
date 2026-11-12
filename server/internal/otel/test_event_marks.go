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

// withoutTestEventMarks drops the test marks from attributes, reporting whether any were there.
func withoutTestEventMarks[KV interface{ GetKey() string }](attributes []KV) ([]KV, bool) {
	before := len(attributes)
	kept := slices.DeleteFunc(attributes, func(kv KV) bool { return isTestEventMarkKey(kv.GetKey()) })
	return kept, len(kept) != before
}

// Producers may not send the test marks, on a record or its resource: a
// destination that filters tests out would silently hide their real data.

func stripLogTestEventMarks(record *otelv1.LogRecord) {
	if kept, changed := withoutTestEventMarks(record.GetAttributes()); changed {
		record.SetAttributes(kept)
	}
	if resource := record.GetResource(); resource != nil {
		if kept, changed := withoutTestEventMarks(resource.GetAttributes()); changed {
			resource.SetAttributes(kept)
		}
	}
}

func stripSpanTestEventMarks(span *otelv1.Span) {
	if kept, changed := withoutTestEventMarks(span.GetAttributes()); changed {
		span.SetAttributes(kept)
	}
	if resource := span.GetResource(); resource != nil {
		if kept, changed := withoutTestEventMarks(resource.GetAttributes()); changed {
			resource.SetAttributes(kept)
		}
	}
}

func stripMetricTestEventMarks(item *otelv1.Metric) {
	if resource := item.GetResource(); resource != nil {
		if kept, changed := withoutTestEventMarks(resource.GetAttributes()); changed {
			resource.SetAttributes(kept)
		}
	}
	for _, point := range item.GetGauge().GetDataPoints() {
		if kept, changed := withoutTestEventMarks(point.GetAttributes()); changed {
			point.SetAttributes(kept)
		}
	}
	for _, point := range item.GetSum().GetDataPoints() {
		if kept, changed := withoutTestEventMarks(point.GetAttributes()); changed {
			point.SetAttributes(kept)
		}
	}
	for _, point := range item.GetHistogram().GetDataPoints() {
		if kept, changed := withoutTestEventMarks(point.GetAttributes()); changed {
			point.SetAttributes(kept)
		}
	}
	for _, point := range item.GetExponentialHistogram().GetDataPoints() {
		if kept, changed := withoutTestEventMarks(point.GetAttributes()); changed {
			point.SetAttributes(kept)
		}
	}
	for _, point := range item.GetSummary().GetDataPoints() {
		if kept, changed := withoutTestEventMarks(point.GetAttributes()); changed {
			point.SetAttributes(kept)
		}
	}
}
