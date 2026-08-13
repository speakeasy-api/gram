package conv

import "strings"

// NormalizeEmail canonicalizes an email address for comparison and map keys.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func TruncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// truncatedDetailNotice tells a reader the text was cut and where the whole of
// it can be found.
const truncatedDetailNotice = "… (truncated, see Gram logs for the full error)"

// TruncateDetail bounds third-party error text that is echoed back to an API
// caller, appending a notice when it had to cut.
//
// The provider's own message is usually the whole value of surfacing an error —
// it names the missing grant, or the resource's real state — so it is passed
// through rather than replaced with something generic. Bounding it keeps an
// unexpectedly chatty SDK error from turning the field into a channel for
// arbitrary internal detail.
//
// The cut is by rune, not byte: provider text is not guaranteed ASCII, and
// slicing a string at a byte offset can land mid-sequence and emit invalid
// UTF-8 into the response.
func TruncateDetail(s string, maxRunes int) string {
	// A negative bound would slice with a negative index and panic. Callers pass a
	// constant today, but this is a shared helper and the failure would be a panic
	// on an error path, which is the worst place to find one.
	if maxRunes < 0 {
		maxRunes = 0
	}

	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}

	return string(runes[:maxRunes]) + truncatedDetailNotice
}
