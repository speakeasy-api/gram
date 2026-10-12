package enrich

import (
	"unicode/utf8"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

// known reports whether a dialect stated a value: it named the attribute the
// value came from and read it without error. A stated zero or empty string
// is still stated.
func known(key string, err error) bool {
	return err == nil && key != ""
}

// stated keeps a dialect's answer only when it stated one.
func stated[T any](key string, value T, err error) T {
	var zero T
	if !known(key, err) {
		return zero
	}
	return value
}

// inboundLogSource derives the source from the resource's service.name the
// way the event feed does, so every consumer calls a producer the same thing.
func inboundLogSource(record *otelv1.InboundLogRecord) string {
	return CanonicalSource(inboundLogResourceString(record, ServiceNameAttribute))
}

func inboundSpanSource(span *otelv1.InboundSpan) string {
	return CanonicalSource(inboundSpanResourceString(span, ServiceNameAttribute))
}

func inboundLogResourceString(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetResource().GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func inboundSpanResourceString(span *otelv1.InboundSpan, key string) string {
	for _, kv := range span.GetResource().GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func inboundLogAttributeString(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func inboundSpanAttributeString(span *otelv1.InboundSpan, key string) string {
	for _, kv := range span.GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// truncateUTF8 cuts s to at most maxBytes on a character boundary.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
