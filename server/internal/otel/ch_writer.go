package otel

import (
	"encoding/hex"
	"math"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

// Helpers shared by the ClickHouse event feed writers
// (handler_log_ch_writer.go and handler_span_ch_writer.go).

// eventSourceUnknown is stored in the source column when a record's resource
// carries no usable service.name.
const eventSourceUnknown = "unknown"

// serviceNameAttribute is the OTel resource attribute the event source is
// derived from.
const serviceNameAttribute = "service.name"

// canonicalEventSource derives the Event Feed source slug from a resource
// service.name. Known product-surface aliases are folded first (via
// chat.CanonicalSource, e.g. ClaudeCode becomes claude-code) so the feed's
// source values line up with the rest of the product, then the result is
// slugified: lowercased with runs of non-alphanumerics collapsed to single
// hyphens. Returns unknown when the name is empty or yields no slug.
func canonicalEventSource(serviceName string) string {
	slug := slugifyEventSource(chat.CanonicalSource(serviceName))
	if slug == "" {
		return eventSourceUnknown
	}
	return slug
}

func slugifyEventSource(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingHyphen := false
	for _, r := range strings.ToLower(s) {
		isAlphanumeric := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isAlphanumeric {
			pendingHyphen = true
			continue
		}
		if pendingHyphen && b.Len() > 0 {
			b.WriteByte('-')
		}
		pendingHyphen = false
		b.WriteRune(r)
	}
	return b.String()
}

// hexEventID hex-encodes an OTLP trace or span id. OTLP treats both empty and
// all-zero ids as absent, so both encode as the empty string.
func hexEventID(id []byte) string {
	for _, b := range id {
		if b != 0 {
			return hex.EncodeToString(id)
		}
	}
	return ""
}

// eventUnixNano converts an OTLP fixed64 nanosecond timestamp to the Int64
// the event feed tables store, clamping the (practically unreachable)
// overflow instead of wrapping negative.
func eventUnixNano(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}
