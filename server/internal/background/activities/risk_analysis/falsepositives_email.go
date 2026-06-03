package risk_analysis

import (
	"strings"
)

// nonPIIEmailReason returns a short, human-readable reason why the given
// email-shaped string should be treated as a non-PII false positive, or
// "" if the string looks like it could be a real email address.
//
// Three layers, in order:
//
//  1. Prefix table for KV / env / config wrappers where the match is
//     fronted by a fixed non-email token (`DB_USERNAME=...`,
//     `identity=...`, etc.). Matched against the lowercased input so
//     the prefixes themselves stay lowercase.
//  2. RFC-shape sanity: `/` is invalid in both the local-part and the
//     domain of an addr-spec, so any candidate containing `/` is a URL
//     or path fragment, not an email. This catches Medium `@user`
//     URLs, GCP IAM API paths, CDN asset URLs, npm / Go / Deno module
//     paths, and anything else that swept an `@` out of a longer
//     string.
//  3. Two narrow domain checks that survive the slash filter:
//     a `.gserviceaccount.com` suffix (GCP machine identity) and a
//     trailing digit on the right-hand side of the final `@` (TLDs are
//     letters; a digit there is a package version suffix like
//     `pkg@v1.2.3`).
//
// Lower-confidence buckets from the offline analysis (JSON-escaped
// angle brackets, ANSI colour codes, Faker localparts, fictional
// company domains, role aliases, GitHub noreply, Anthropic
// transactional no-reply, etc.) are intentionally NOT filtered: each
// has plausible real-world matches we'd rather over-report than miss.
func nonPIIEmailReason(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)

	for _, p := range nonPIIEmailPrefixes {
		if strings.HasPrefix(lower, p.prefix) {
			return p.description
		}
	}

	if strings.Contains(trimmed, "/") {
		return "contains '/' (not valid in an addr-spec)"
	}

	if strings.HasSuffix(lower, ".gserviceaccount.com") ||
		strings.Contains(lower, "@cloudservices.gserviceaccount") {
		return "gcp service account"
	}

	if at := strings.LastIndex(trimmed, "@"); at >= 0 && at < len(trimmed)-1 {
		if last := trimmed[len(trimmed)-1]; last >= '0' && last <= '9' {
			return "domain ends in digit (likely version suffix)"
		}
	}

	return ""
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
