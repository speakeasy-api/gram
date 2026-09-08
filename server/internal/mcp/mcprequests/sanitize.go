package mcprequests

import (
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// MaxClientInfoFieldLength bounds each client-supplied metadata field
// (client name, client version, capability key) retained from a request.
const MaxClientInfoFieldLength = 100

// SanitizeClientInfoField bounds one untrusted client-reported metadata
// field. Invalid UTF-8 and control characters are dropped, surrounding
// whitespace is trimmed so equivalent spellings collapse to one value, and
// the result is capped at [MaxClientInfoFieldLength], so the value stays safe
// to store, to record as an analytics property, and to hand to a function
// runner.
func SanitizeClientInfoField(value string) string {
	cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "")))

	return conv.TruncateString(cleaned, MaxClientInfoFieldLength)
}
