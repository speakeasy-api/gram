package otel

import (
	"encoding/hex"
	"math"
)

// Helpers shared by the ClickHouse event feed writers
// (handler_log_ch_writer.go and handler_span_ch_writer.go).

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
