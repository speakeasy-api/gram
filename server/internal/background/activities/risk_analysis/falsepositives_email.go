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
//  1. Reserved / placeholder domain — the primary reason this filter
//     exists. Two sub-cases:
//     a. RFC 6761 special-use TLDs (`.example`, `.invalid`,
//     `.localhost`, `.test`). Anything under one of these is
//     guaranteed not to be a real address.
//     b. Widely-used fixture SLDs paired with a real-world TLD
//     (`example.com`, `acme.com`, `acmecorp.com`, `asdf.com`, etc.).
//     These are not PII the policy author cares about regardless of
//     the local-part.
//  2. Any `/` in the candidate. Per RFC 5322 §3.2.3 atext does include
//     `/`, so `/` is technically permitted in the local-part of an
//     addr-spec. In practice every `/`-containing match in our corpus
//     is a URL fragment (`medium.com/@user`, GCP IAM API paths, CDN
//     asset URLs, npm / Go / Deno module paths) rather than an email
//     with a slash in the local-part. We accept the theoretical miss
//     for the URL-noise drop.
//  3. Two narrow domain checks that survive the slash filter:
//     a `.gserviceaccount.com` suffix (GCP machine identity) and a
//     trailing digit on the right-hand side of the final `@` (TLDs are
//     letters; a digit there is a package version suffix like
//     `pkg@v1.2.3`).
//  4. Local-parts that can never identify a real person: template
//     tokens (`first.last`, `firstname.lastname`) and the universally
//     automated `noreply` / `no-reply` aliases. Canonical placeholder
//     person names like `john.doe` or `joe.bloggs` are NOT in this set
//     because real people share them.
//
// Lower-confidence buckets from the offline analysis (KV / env / config
// wrappers like `DB_USERNAME=…`, JSON-escaped angle brackets, ANSI
// colour codes, Faker localparts on real-shape domains, role aliases,
// GitHub noreply, Anthropic transactional no-reply, etc.) are
// intentionally NOT filtered: each has plausible real-world matches
// we'd rather over-report than miss. KV wrappers in particular tend
// to wrap real production emails, so dropping them would mask PII.
func nonPIIEmailReason(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)

	if r := placeholderDomainReason(lower); r != "" {
		return r
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
	"acmestore":   true,
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

// placeholderLocalParts is the set of local-parts that can never
// identify a real person: template tokens (`first.last`,
// `firstname.lastname`) and the universally automated `noreply` /
// `no-reply` aliases. Matched against the lowercased local-part
// (everything before the final `@`).
//
// Canonical placeholder person names like `john.doe`, `jane.doe`, and
// `joe.bloggs` are deliberately NOT included: real people share those
// names and the corpus benefit is not worth the over-filter risk.
var placeholderLocalParts = map[string]bool{
	"first.last":         true,
	"firstname.lastname": true,
	"noreply":            true,
	"no-reply":           true,
}
