package maskdisplay_test

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
)

func TestDisplay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		ruleID string
		match  string
		want   string
	}{
		// Rule 1: empty match.
		{name: "empty match", source: "gitleaks", ruleID: "secret.token", match: "", want: ""},
		{name: "empty match wins over shadow_mcp passthrough", source: "shadow_mcp", ruleID: "shadow.server", match: "", want: ""},

		// Rule 2: judge/derived sources display nothing.
		{name: "prompt_injection is empty", source: "prompt_injection", ruleID: "injection.detected", match: "ignore previous instructions", want: ""},
		{name: "llm_judge is empty", source: "llm_judge", ruleID: "policy.custom", match: "some flagged content", want: ""},
		{name: "destructive_tool is empty", source: "destructive_tool", ruleID: "tool.destructive", match: "drop_database", want: ""},
		{name: "cli_destructive is empty", source: "cli_destructive", ruleID: "cli.rm_rf", match: "rm -rf /", want: ""},

		// Rule 3: shadow_mcp passes through verbatim.
		{name: "shadow_mcp verbatim", source: "shadow_mcp", ruleID: "shadow.server", match: "npx some-mcp-server", want: "npx some-mcp-server"},
		{name: "shadow_mcp verbatim multibyte", source: "shadow_mcp", ruleID: "shadow.server", match: "sërver-ü", want: "sërver-ü"},

		// Rule 4: emails show only the domain, with a fixed three-star local part.
		{name: "email rule", source: "presidio", ruleID: "pii.email_address", match: "alice@example.com", want: "***@example.com"},
		{name: "email rule long local part does not leak length", source: "presidio", ruleID: "pii.email_address", match: "a.much.longer.local.part@example.com", want: "***@example.com"},
		{name: "email rule splits at last at-sign", source: "presidio", ruleID: "pii.email_address", match: "a@b@c.io", want: "***@c.io"},
		{name: "email rule trailing at-sign", source: "presidio", ruleID: "pii.email_address", match: "alice@", want: "***@"},
		{name: "email rule multibyte domain", source: "presidio", ruleID: "pii.email_address", match: "usér@exämple.com", want: "***@exämple.com"},
		{name: "account_identity email", source: "account_identity", ruleID: "account.personal", match: "bob@corp.io", want: "***@corp.io"},
		// Rule 4 fall-through: no "@" means general tiers apply.
		{name: "email rule without at-sign uses general tier", source: "presidio", ruleID: "pii.email_address", match: "aliceexample", want: "alic******le"},
		{name: "account_identity without at-sign uses general tier", source: "account_identity", ruleID: "account.personal", match: "personal-acct", want: "pers*******ct"},

		// Rule 5: financial category keeps only the last four runes.
		{name: "financial credit card", source: "presidio", ruleID: "pii.credit_card", match: "4111111111111111", want: "****1111"},
		{name: "financial iban", source: "presidio", ruleID: "pii.iban_code", match: "DE89370400440532013000", want: "****3000"},
		{name: "financial at boundary n=5", source: "presidio", ruleID: "pii.us_bank_number", match: "12345", want: "****2345"},
		// Financial shorter than 5 runes falls back to the general tiers.
		{name: "financial n=4 uses general tier", source: "presidio", ruleID: "pii.credit_card", match: "1234", want: "1**4"},

		// Rule 6: general tiers, including every boundary (n=2,3,4,5,7,8).
		{name: "general n=20 aws key", source: "gitleaks", ruleID: "aws-access-token", match: "AKIAIOSFODNN7EXAMPLE", want: "AKIA**************LE"},
		{name: "general n=8", source: "gitleaks", ruleID: "secret.token", match: "abcdefgh", want: "abcd**gh"},
		{name: "general n=7", source: "gitleaks", ruleID: "secret.token", match: "abcdefg", want: "ab****g"},
		{name: "general n=5", source: "gitleaks", ruleID: "secret.token", match: "abcde", want: "ab**e"},
		{name: "general n=4", source: "gitleaks", ruleID: "secret.token", match: "abcd", want: "a**d"},
		{name: "general n=3", source: "gitleaks", ruleID: "secret.token", match: "abc", want: "a*c"},
		{name: "general n=2 fully masked", source: "gitleaks", ruleID: "secret.token", match: "ab", want: "**"},
		{name: "general n=1 fully masked", source: "gitleaks", ruleID: "secret.token", match: "a", want: "*"},

		// Multibyte runes are never cut mid-rune and count as one each.
		{name: "multibyte n=12", source: "presidio", ruleID: "pii.person", match: "héllo wörld!", want: "héll******d!"},
		{name: "multibyte n=3", source: "presidio", ruleID: "pii.person", match: "日本語", want: "日*語"},
		{name: "multibyte n=2 fully masked", source: "presidio", ruleID: "pii.person", match: "éü", want: "**"},

		// Unknown sources and rules still mask through the general tiers.
		{name: "custom rule general tier", source: "custom", ruleID: "custom.internal_id", match: "ID-00042-XYZ", want: "ID-0******YZ"},
	}

	for _, tc := range cases {
		got := maskdisplay.Display(tc.source, tc.ruleID, tc.match)
		require.Equal(t, tc.want, got, "case %q: Display(%q, %q, %q)", tc.name, tc.source, tc.ruleID, tc.match)

		// Guard the rune-based slicing: a byte-sliced implementation would emit
		// partial runes (invalid UTF-8) on multibyte input.
		require.True(t, utf8.ValidString(got), "case %q produced invalid UTF-8", tc.name)
	}
}
