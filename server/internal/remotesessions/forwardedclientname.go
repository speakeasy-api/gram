package remotesessions

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// RFC 6749 §8.2 asks unregistered parameters to carry a vendor prefix.
	forwardedClientNameParam = "speakeasy_client_name"
	// Matches the limit on client_name in a client metadata document.
	maxForwardedClientNameBytes = 256
)

// sanitizeForwardedClientName drops control and invisible characters and caps the name.
func sanitizeForwardedClientName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(name, "") {
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Other_Default_Ignorable_Code_Point, unicode.Variation_Selector) {
			continue
		}
		if b.Len()+utf8.RuneLen(r) > maxForwardedClientNameBytes {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
