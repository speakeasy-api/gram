package httpcache

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// cacheControlDelta extracts a delta-seconds directive from every
// Cache-Control header on the response. RFC 9110 §5.3 lets a field repeat
// across lines, so Values is read rather than Get, which would see only the
// first line.
//
// A malformed or overflowing value reports not-found rather than a zero
// duration, so the caller falls through to the next source of freshness
// instead of pinning the entry at the floor.
func cacheControlDelta(header http.Header, directive string) (time.Duration, bool) {
	for _, value := range header.Values("Cache-Control") {
		for _, candidate := range splitDirectives(value) {
			name, arg, found := strings.Cut(strings.TrimSpace(candidate), "=")
			if !found || !strings.EqualFold(strings.TrimSpace(name), directive) {
				continue
			}
			if seconds, ok := deltaSeconds(arg); ok {
				return seconds, true
			}
		}
	}
	return 0, false
}

// splitDirectives splits one Cache-Control field value on the commas that
// separate directives, leaving commas inside a quoted-string alone.
//
// Splitting naively would let a quoted argument masquerade as a directive:
// no-cache="Set-Cookie, max-age=99999" breaks into a fragment reading
// `max-age=99999"`, which is a lifetime the host never granted. Directives
// that take a quoted field-name list (no-cache, private) are exactly the ones
// where this arises, and the field value is chosen by the document host.
func splitDirectives(value string) []string {
	directives := make([]string, 0, strings.Count(value, ",")+1)
	quoted := false
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			// RFC 9110 §5.6.4 quoted-pair: the next octet is literal, so it
			// can neither open nor close the string.
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				directives = append(directives, value[start:i])
				start = i + 1
			}
		}
	}
	// An unterminated quoted-string means the field value is malformed and
	// its directive boundaries are guesswork. Discarding it entirely keeps
	// the same rule the rest of this parser follows: what cannot be read as
	// the host meant it is treated as no opinion, not as a partial one.
	if quoted {
		return nil
	}
	return append(directives, value[start:])
}

// deltaSeconds parses an RFC 9111 §1.2.2 delta-seconds value into a duration.
// The grammar belongs to Cache-Control's directives and is reused by the Age
// header. It reports not-found for anything negative, unparseable, or large
// enough to overflow: the value is attacker-supplied and time.Duration is
// nanoseconds, so an unguarded multiplication would wrap into a negative
// duration that the clamp would silently read as "expire immediately".
func deltaSeconds(raw string) (time.Duration, bool) {
	digits, ok := deltaSecondsDigits(raw)
	if !ok {
		return 0, false
	}

	// Guard the multiplication rather than the result: time.Duration is
	// nanoseconds, so a value past this bound overflows into a negative
	// duration that the clamp would silently read as "expire immediately".
	seconds, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || seconds > math.MaxInt64/int64(time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// deltaSecondsDigits returns the digits of a well-formed delta-seconds value,
// reporting false for anything that is not one. The grammar is 1*DIGIT, so a
// sign is not part of it: strconv would happily read "+7200" and "-0", and
// honouring either would mean acting on a lifetime the host did not express.
//
// It reports true for digit strings too large to be a duration, which is why
// it is separate from deltaSeconds — the two callers want opposite fallbacks
// for an overflow.
func deltaSecondsDigits(raw string) (string, bool) {
	value := strings.TrimSpace(raw)

	// RFC 9110 §5.6.6 allows a directive argument to be sent as a
	// quoted-string even when the grammar calls for a token, but only as a
	// matched pair. Stripping stray quotes instead would accept a value the
	// upstream never expressed.
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}

	if value == "" {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return "", false
		}
	}
	return value, true
}
