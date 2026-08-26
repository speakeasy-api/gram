package researchagent

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// emailPattern matches email-shaped tokens for briefing redaction. Redaction
// is a minimization pass, not a security boundary, so a plain conservative
// pattern is the right tool: an exotic address that slips through costs a
// little privacy, while an over-eager pattern eats evidence.
var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// redactEmails replaces every email in s with a stable per-address
// placeholder, so the text still says how many distinct people it mentions
// without saying who any of them are. The numbering is first-appearance
// order within s.
func redactEmails(s string) string {
	placeholders := make(map[string]string)
	return emailPattern.ReplaceAllStringFunc(s, func(email string) string {
		placeholder, seen := placeholders[email]
		if !seen {
			placeholder = fmt.Sprintf("person-%d@redacted.invalid", len(placeholders)+1)
			placeholders[email] = placeholder
		}
		return placeholder
	})
}

// harvestHTTPSURLs scans trusted text for https URLs, for seeding the fetch
// menu from the briefing. The scan is deliberately simple — take each
// https:// run up to a delimiter, then shed the punctuation that prose and
// JSON wrap URLs in — because the menu's canonicalization discards anything
// that does not survive parsing anyway. Quotes and angle brackets end a
// token like whitespace does: briefing evidence is JSON, often compact, and
// a quote-delimited URL must not swallow the fields after it.
func harvestHTTPSURLs(text string) []string {
	var urls []string
	for remaining := text; ; {
		start := strings.Index(remaining, "https://")
		if start < 0 {
			break
		}
		candidate := remaining[start:]
		end := strings.IndexFunc(candidate, func(r rune) bool {
			return unicode.IsSpace(r) || r == '"' || r == '<' || r == '>' || r == '\\'
		})
		if end < 0 {
			end = len(candidate)
		}
		token := trimTrailingURLPunct(candidate[:end])
		if token != "https://" {
			urls = append(urls, token)
		}
		remaining = candidate[min(end, len(candidate)):]
	}

	return urls
}

// trimTrailingURLPunct sheds the punctuation prose and JSON wrap URLs in,
// while keeping a close paren that the URL itself opened — Wikipedia-style
// paths legitimately end in ")".
func trimTrailingURLPunct(token string) string {
	for len(token) > 0 {
		last := token[len(token)-1]
		if !strings.ContainsRune(".,;:!?)'`]}", rune(last)) {
			break
		}
		if last == ')' && strings.Count(token, "(") >= strings.Count(token, ")") {
			break
		}
		token = token[:len(token)-1]
	}

	return token
}
