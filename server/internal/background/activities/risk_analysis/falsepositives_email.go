package risk_analysis

import (
	"strings"
)

// nonPIIEmailReason returns a short, human-readable reason why the given
// email-shaped string should be treated as a non-PII false positive, or
// "" if the string looks like it could be a real email address.
//
// Five layers, in order:
//
//  1. Reserved / placeholder domain — the primary reason this filter
//     exists. Two sub-cases:
//     a. RFC 6761 special-use TLDs (`.example`, `.invalid`,
//     `.localhost`, `.test`). Anything under one of these is
//     guaranteed not to be a real address.
//     b. Widely-used fixture SLDs paired with a real-world TLD
//     (`example.com`, `acme.com`, `acmecorp.com`, `asdf.com`, etc.).
//     These are not PII the policy author cares about regardless of
//     the local-part.
//  2. Prefix table for KV / env / config wrappers where the match is
//     fronted by a fixed non-email token (`DB_USERNAME=...`,
//     `identity=...`, etc.). Matched against the lowercased input so
//     the prefixes themselves stay lowercase.
//  3. Any `/` in the candidate. Per RFC 5322 §3.2.3 atext does include
//     `/`, so `/` is technically permitted in the local-part of an
//     addr-spec. In practice every `/`-containing match in our corpus
//     is a URL fragment (`medium.com/@user`, GCP IAM API paths, CDN
//     asset URLs, npm / Go / Deno module paths) rather than an email
//     with a slash in the local-part. We accept the theoretical miss
//     for the URL-noise drop.
//  4. Two narrow domain checks that survive the slash filter:
//     a `.gserviceaccount.com` suffix (GCP machine identity) and a
//     trailing digit on the right-hand side of the final `@` (TLDs are
//     letters; a digit there is a package version suffix like
//     `pkg@v1.2.3`).
//  5. Canonical placeholder local-parts (`john.doe`, `jane.doe`,
//     `joe.bloggs`, `first.last`, etc.) even on real-shape domains.
//     These are widely-used textbook fixture names from documentation,
//     Faker output, and tutorial seed data. Real people share these
//     names, so we accept the theoretical miss for the noise drop.
//
// Lower-confidence buckets from the offline analysis (JSON-escaped
// angle brackets, ANSI colour codes, generic Faker localparts on
// real-shape domains, role aliases, GitHub noreply, Anthropic
// transactional no-reply, etc.) are intentionally NOT filtered: each
// has plausible real-world matches we'd rather over-report than miss.
func nonPIIEmailReason(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)

	if r := placeholderDomainReason(lower); r != "" {
		return r
	}

	for _, p := range nonPIIEmailPrefixes {
		if strings.HasPrefix(lower, p.prefix) {
			return p.description
		}
	}

	if strings.Contains(trimmed, "/") {
		return "contains '/' (URL or path fragment)"
	}

	if strings.HasSuffix(lower, ".gserviceaccount.com") {
		return "gcp service account"
	}

	if at := strings.LastIndex(trimmed, "@"); at >= 0 && at < len(trimmed)-1 {
		if last := trimmed[len(trimmed)-1]; last >= '0' && last <= '9' {
			return "domain ends in digit (likely version suffix)"
		}
	}

	if at := strings.LastIndex(lower, "@"); at > 0 {
		if placeholderLocalParts[lower[:at]] {
			return "fixture / placeholder local-part"
		}
	}

	return ""
}

// placeholderDomainReason reports whether the right-hand side of the
// final `@` is a reserved or fixture domain. lower is the
// already-lowercased input. Returns the matching category or "".
//
// Two sub-checks, in order:
//   - The trailing label is an RFC 6761 special-use TLD (`.example`,
//     `.invalid`, `.localhost`, `.test`). This applies regardless of
//     subdomain depth: both `user@test` and `user@host.test` match.
//   - The second-level label is in placeholderSLDs and the top-level
//     label is in placeholderTLDs. Subdomain depth is irrelevant.
//
// `test`, `invalid`, and `localhost` are NOT in placeholderSLDs because
// `test.com`, `invalid.com`, etc. are real registered domains; only
// their use as TLDs is reserved.
func placeholderDomainReason(lower string) string {
	at := strings.LastIndex(lower, "@")
	if at < 0 || at >= len(lower)-1 {
		return ""
	}
	parts := strings.Split(lower[at+1:], ".")
	tld := parts[len(parts)-1]
	if reservedSpecialTLDs[tld] {
		return "RFC 6761 reserved special-use TLD"
	}
	if len(parts) < 2 {
		return ""
	}
	sld := parts[len(parts)-2]
	if placeholderSLDs[sld] && placeholderTLDs[tld] {
		return "fixture / placeholder domain"
	}
	return ""
}

// reservedSpecialTLDs lists the top-level domains reserved by RFC 6761
// for special use. Anything ending in one of these labels is
// guaranteed not to resolve to a public mailbox.
var reservedSpecialTLDs = map[string]bool{
	"example":   true,
	"invalid":   true,
	"localhost": true,
	"test":      true,
}

// placeholderSLDs is the set of second-level domains conventionally
// used for fixtures and "obviously fake" corporate examples.
// `example` is included because example.com / .org / .net are
// specifically reserved by RFC 2606. The rest are widely-used
// community conventions seen in Faker output, SDK docs, seed data, and
// policy fixtures.
var placeholderSLDs = map[string]bool{
	"example":     true,
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

// placeholderLocalParts is the set of canonical fixture / textbook
// person-name local-parts conventionally used in documentation, Faker
// output, tutorials, and seed data. Matched against the lowercased
// local-part (everything before the final `@`).
//
// Real people share these names, so this is an explicit
// precision-leaning miss for the noise drop. We only include
// well-known placeholders, not generic Faker output like
// `alice.brown` or `Chadrick_Quigley52`.
var placeholderLocalParts = map[string]bool{
	"john.doe":           true,
	"jane.doe":           true,
	"joe.bloggs":         true,
	"joe.blogs":          true,
	"first.last":         true,
	"firstname.lastname": true,
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
