package risk_analysis

import (
	"regexp"
	"strings"
)

// nonPIIEmailReason returns a short, human-readable reason why the given
// email-shaped string should be treated as a non-PII false positive, or
// "" if the string looks like a real human email address.
//
// Derived from analysis of ~40k pii.email_address findings across the
// moonpay and speakeasy projects, where ~55% of unique matches were not
// real human emails. The patterns covered here are the ones where the
// "match" is either log-format noise, a machine identity, a fixture
// placeholder, or a non-email substring (URL, package path, KV pair)
// that happens to contain an "@".
//
// Layers, in order of evaluation:
//
//  1. Wrapper / log-format noise — the match is a real email with a log
//     prefix glued on (Presidio's own log rows, JSON-escaped angle
//     brackets, ANSI colour codes, UUID record IDs).
//  2. Non-email shapes — KV pairs (DB_USERNAME=…), template placeholders
//     ({TOKEN}@…, %s@…), URLs containing "@" (medium.com/@user,
//     iam.googleapis.com/.../serviceAccounts/…, Slack emoji assets), and
//     package paths with @version suffix (pkg@v1.2.3).
//  3. Machine identities — *.gserviceaccount.com, Anthropic transactional
//     no-reply hashes, GitHub SSH user, CI runner hostnames, Google
//     Calendar group IDs, Apple Hide My Email relay.
//  4. Fixture domains — *.example.com, *.test.com, asdf.com and similar
//     well-known placeholder TLDs.
//
// Lower-confidence buckets from the offline analysis (Faker localparts
// like john.doe, fictional domains like @acme.com, generic role aliases
// like support@) are intentionally NOT filtered here: each one has
// plausible real-world matches that we'd rather over-report than miss.
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
	for _, p := range nonPIIEmailPatterns {
		if p.rx.MatchString(trimmed) {
			return p.description
		}
	}
	return ""
}

type emailPrefixHit struct {
	prefix      string // matched against the lowercased input
	description string
}

type emailPatternHit struct {
	rx          *regexp.Regexp
	description string
}

// nonPIIEmailPrefixes catches matches where the email is fronted by
// log-format noise or a non-email key. Prefix is matched against the
// lowercased input.
var nonPIIEmailPrefixes = []emailPrefixHit{
	// Presidio's own log/audit rows: "npresidio|EMAIL_ADDRESS|<offset>|<recid>|<email>".
	{"npresidio|", "presidio log-row wrapper"},
	{"presidio|", "presidio log-row wrapper"},

	// JSON-escaped angle brackets from "<email@x>" / ">email@x" RFC822
	// address forms that survived JSON encoding without re-decoding.
	{"u003c", "json-escaped < prefix"},
	{"u003e", "json-escaped > prefix"},

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

	// URL fragments where "@" is part of the URL, not an address.
	{"medium.com/@", "medium url"},
	{"mail.google.com/", "gmail url"},
	{"www.postgresql.org/", "postgres mailing list url"},
	{"iam.googleapis.com/", "gcp iam api path"},
	{"a.slack-edge.com/", "slack cdn asset"},
	{"app.datadoghq.eu/", "datadog query url"},
	{"cdn.jsdelivr.net/", "jsdelivr cdn asset"},
	{"unpkg.com/", "unpkg cdn asset"},
	{"esm.sh/", "esm.sh cdn asset"},
	{"deno.land/", "deno module path"},
	{"pkg.go.dev/", "go module path"},

	// Go module import paths with the canonical version suffix on the
	// far end. Catches "github.com/foo/bar@v1.2.3", etc.; the
	// version-suffix regex below catches the rest.
	{"github.com/", "go module path"},
	{"gitlab.com/", "go module path"},
	{"golang.org/", "go module path"},
	{"cloud.google.com/", "go module path"},
	{"goa.design/", "go module path"},
	{"honnef.co/", "go module path"},
	{"go.opentelemetry.io/", "go module path"},
	{"go.temporal.io/", "go module path"},
	{"git.example.com/", "go module path"},
}

// nonPIIEmailPatterns catches FP shapes that are not anchorable to a
// single prefix: domain-suffix matches and end-of-string version
// suffixes. Ordered by specificity.
var nonPIIEmailPatterns = []emailPatternHit{
	// UUID|email and similar record-id prefix on a real email.
	{regexp.MustCompile(`^[0-9a-fA-F][0-9a-fA-F-]{7,}\|.*@`), "record-id pipe prefix"},

	// ANSI colour code "\e[170m" stripped of the escape and bracket
	// (e.g. "170madam@speakeasy.com"). 1–4 digits followed by "m" then a
	// lowercase letter.
	{regexp.MustCompile(`^\d{1,4}m[a-z]`), "ansi colour code prefix"},

	// "@version" suffix on a package / module path.
	// Matches v1.2.3, 5.2.1, v0.0.0-20260223084236-ed0328a0a462, etc.
	{regexp.MustCompile(`@v?\d+(\.\d+)+([-+][A-Za-z0-9.-]+)?$`), "package version suffix"},

	// Template placeholders that still contain literal {VAR} / ${VAR} /
	// %s tokens.
	{regexp.MustCompile(`\{\{|\}\}|\$\{|^[A-Z_]+@|^[A-Z_]+\}@|\}@|^TOKEN@|%[sd]@|\{[A-Za-z_][A-Za-z0-9_]*\}@`), "unrendered template placeholder"},

	// Slack emoji asset URL: ".../1f4a1@2x.png".
	{regexp.MustCompile(`@\d+x\.(png|jpg|jpeg|gif|svg|webp)$`), "cdn asset url"},

	// Google Calendar group identifier.
	{regexp.MustCompile(`@group\.calendar\.google\.com$`), "google calendar group id"},

	// Any GCP IAM / cloud service account.
	{regexp.MustCompile(`(?i)\.gserviceaccount\.com$`), "gcp service account"},
	{regexp.MustCompile(`(?i)@cloudservices\.gserviceaccount`), "gcp service account"},

	// Anthropic transactional no-reply (per-message random suffix).
	{regexp.MustCompile(`^no-reply-[A-Za-z0-9_-]+@mail\.anthropic\.com$`), "anthropic transactional no-reply"},

	// GitHub SSH user; not a real recipient mailbox.
	{regexp.MustCompile(`^[nt]?git@(github|gitlab)\.com$|^octocat@github\.com$`), "ssh git user"},

	// GitHub's own noreply suffix.
	{regexp.MustCompile(`@users\.noreply\.github\.com$`), "github noreply user"},

	// Blacksmith / GitHub Actions runner hostnames.
	{regexp.MustCompile(`@blacksmith-scale|\.vm\.blacksmith\.sh$`), "ci runner hostname"},

	// Apple Hide My Email relay.
	{regexp.MustCompile(`@privaterelay\.appleid\.com$`), "apple private relay"},

	// Fixture / placeholder domains: *.example.com, *.test.com, plus a
	// short list of universally-recognised "not a real domain" TLDs.
	{regexp.MustCompile(`(?i)@([a-z0-9-]+\.)*(example|test|asdf|fake|invalid|localhost|nowhere|placeholder|sample|dummy|yourorg)\.(com|org|net|local|dev|io)$`), "fixture / placeholder domain"},

	// TypeSpec / OpenAPI / Temporal route artifacts surfaced as
	// "n@TypeSpec.OpenAPI.info", "n@app.post", etc.
	{regexp.MustCompile(`^n@(TypeSpec|app|temporal|user|bomb)\.`), "schema / route artifact"},

	// Hash-id mailboxes used as one-time tracking addresses.
	{regexp.MustCompile(`^[a-f0-9]{24,}@`), "hash-id tracking mailbox"},
	{regexp.MustCompile(`^c_[a-z0-9]{20,}@`), "google calendar group id"},
}
