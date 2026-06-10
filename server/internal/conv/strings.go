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
