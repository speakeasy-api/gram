package risk_analysis

import (
	"strings"
)

// nonPIIEmailReason returns a short, human-readable reason why the given
// email-shaped string should be treated as a non-PII false positive, or
// "" if the string looks like it could be a real email address.
//
// Four layers, in order:
//
//  1. Placeholder / fixture domain — the primary reason this filter
//     exists. Documentation, fixture data, seed data, sample SDK
//     snippets, and policy authors' own examples overwhelmingly use a
//     small set of fake corporate domains (`example.com`, `acme.com`,
//     `acmecorp.com`, etc.). These are not PII the policy author cares
//     about, regardless of the local-part.
//  2. Prefix table for KV / env / config wrappers where the match is
//     fronted by a fixed non-email token (`DB_USERNAME=...`,
//     `identity=...`, etc.). Matched against the lowercased input so
//     the prefixes themselves stay lowercase.
//  3. RFC-shape sanity: `/` is invalid in both the local-part and the
//     domain of an addr-spec, so any candidate containing `/` is a URL
//     or path fragment, not an email. This catches Medium `@user`
//     URLs, GCP IAM API paths, CDN asset URLs, npm / Go / Deno module
//     paths, and anything else that swept an `@` out of a longer
//     string.
//  4. Two narrow domain checks that survive the slash filter:
//     a `.gserviceaccount.com` suffix (GCP machine identity) and a
//     trailing digit on the right-hand side of the final `@` (TLDs are
//     letters; a digit there is a package version suffix like
//     `pkg@v1.2.3`).
//
// Lower-confidence buckets from the offline analysis (JSON-escaped
// angle brackets, ANSI colour codes, Faker localparts on real-shape
// domains, role aliases, GitHub noreply, Anthropic transactional
// no-reply, etc.) are intentionally NOT filtered: each has plausible
// real-world matches we'd rather over-report than miss.
func nonPIIEmailReason(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)

	if isPlaceholderDomain(lower) {
		return "fixture / placeholder domain"
	}

	for _, p := range nonPIIEmailPrefixes {
		if strings.HasPrefix(lower, p.prefix) {
			return p.description
		}
	}

	if strings.Contains(trimmed, "/") {
		return "contains '/' (not valid in an addr-spec)"
	}

	if strings.HasSuffix(lower, ".gserviceaccount.com") ||
	if strings.HasSuffix(lower, ".gserviceaccount.com") ||
		strings.HasSuffix(lower, "@cloudservices.gserviceaccount.com") {
		return "gcp service account"
	}

	if at := strings.LastIndex(trimmed, "@"); at >= 0 && at < len(trimmed)-1 {
		if last := trimmed[len(trimmed)-1]; last >= '0' && last <= '9' {
			return "domain ends in digit (likely version suffix)"
		}
	}

	return ""
}

// isPlaceholderDomain reports whether the right-hand side of the final
// `@` looks like a well-known fixture / placeholder domain. lower is
// the already-lowercased input.
//
// Matches when the second-level domain is in placeholderSLDs and the
// top-level domain is in placeholderTLDs, accepting any number of
// subdomains (so both `acme.com` and `mail.dev.acme.com` are caught).
func isPlaceholderDomain(lower string) bool {
	at := strings.LastIndex(lower, "@")
	if at < 0 || at >= len(lower)-1 {
		return false
	}
	parts := strings.Split(lower[at+1:], ".")
	if len(parts) < 2 {
		return false
	}
	sld := parts[len(parts)-2]
	tld := parts[len(parts)-1]
	return placeholderSLDs[sld] && placeholderTLDs[tld]
}

// placeholderSLDs is the set of second-level domains conventionally
// reserved for documentation, fixtures, and "obviously fake" corporate
// examples. `example`, `test`, `invalid`, and `localhost` are
// RFC 6761 reserved special-use names; the rest are widely-used
// community conventions.
var placeholderSLDs = map[string]bool{
	"example":     true,
	"test":        true,
	"invalid":     true,
	"localhost":   true,
	"asdf":        true,
	"fake":        true,
	"nowhere":     true,
	"placeholder": true,
	"sample":      true,
	"dummy":       true,
	"yourorg":     true,
	"acme":        true,
	"acmecorp":    true,
}

// placeholderTLDs is the set of top-level domains placeholder SLDs
// commonly appear under. Anything else (e.g. `acme.co.uk`) is left
// through; the precision-over-recall posture for this filter.
var placeholderTLDs = map[string]bool{
	"com":   true,
	"org":   true,
	"net":   true,
	"local": true,
	"dev":   true,
	"io":    true,
}

type emailPrefixHit struct {
	prefix      string // matched against the lowercased input
	description string
}

// nonPIIEmailPrefixes catches matches where the email is fronted by a
// fixed non-email key. Prefix is matched against the lowercased input.
var nonPIIEmailPrefixes = []emailPrefixHit{
	// KV / env / config fragments where the match is "KEY=email" or
	// "KEY='email"; the email may be real but the surrounding text is
	// not PII the policy author cares about.
	{"db_username=", "env var assignment"},
	{"db_password=", "env var assignment"},
	{"self_name=", "env var assignment"},
	{"email=", "kv pair assignment"},
	{"email_addr=", "kv pair assignment"},
	{"email_address=", "kv pair assignment"},
	{"email_format=", "kv pair assignment"},
	{"identity=", "kv pair assignment"},
	{"user=", "kv pair assignment"},
	{"author=", "kv pair assignment"},
	{"service-account=", "kv pair assignment"},
	{"smtp.mailfrom=", "smtp envelope field"},
	{"placeholder=", "kv pair assignment"},
	{"search=", "query-string fragment"},
	{"ou=", "ldap distinguished name fragment"},
	// Same KV shapes after a leading \n (newline glued on by log frames).
	{"nclaude_code_user_email=", "env var assignment"},
	{"njellyfin_email=", "env var assignment"},
	{"nself_name=", "env var assignment"},
}
