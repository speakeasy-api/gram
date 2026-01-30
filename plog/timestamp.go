package plog

import (
	"time"
)

// Common timestamp formats to try when parsing.
var timestampFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

// parseTimestamp parses a timestamp from a JSON value.
// It handles RFC3339 formats and Unix timestamps (seconds, milliseconds, microseconds, nanoseconds).
func parseTimestamp(v any) time.Time {
	switch val := v.(type) {
	case string:
		return parseTimestampString(val)
	case float64:
		return parseUnixTimestamp(val)
	case int64:
		return parseUnixTimestamp(float64(val))
	default:
		return time.Time{}
	}
}

// parseTimestampString tries to parse a timestamp string using common formats.
func parseTimestampString(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	for _, format := range timestampFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// parseUnixTimestamp parses a Unix timestamp, auto-detecting the unit.
// - Values < 1e10 are treated as seconds
// - Values < 1e13 are treated as milliseconds
// - Values < 1e16 are treated as microseconds
// - Otherwise treated as nanoseconds
func parseUnixTimestamp(f float64) time.Time {
	if f == 0 {
		return time.Time{}
	}

	// Detect the unit based on magnitude
	switch {
	case f < 1e10:
		// Seconds (timestamps before 2001 are rare, but possible)
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec)
	case f < 1e13:
		// Milliseconds
		ms := int64(f)
		return time.UnixMilli(ms)
	case f < 1e16:
		// Microseconds
		us := int64(f)
		return time.UnixMicro(us)
	default:
		// Nanoseconds
		ns := int64(f)
		return time.Unix(0, ns)
	}
}
