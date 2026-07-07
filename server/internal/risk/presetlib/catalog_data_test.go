package presetlib

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests exercise the EMBEDDED data/catalog.yaml through the public
// Reason() API (Track B's data), as opposed to catalog_test.go which drives the
// engine on synthetic in-memory rules. Each case is either:
//
//   - a documented false-positive value that MUST be suppressed
//     (wantReason == true → Reason(m) != ""), or
//   - a realistic REAL value on the SAME rule/source that MUST survive
//     (wantReason == false → Reason(m) == "").
//
// The paired real cases are the over-suppression guard: they prove a rule is
// scoped tightly enough that a live secret / real PII on the same detector is
// not accidentally dropped.

// TestCatalogData_Validate ensures the shipped catalog parses and compiles.
func TestCatalogData_Validate(t *testing.T) {
	t.Parallel()
	lib := newLibrary(t)
	require.Len(t, lib.Version(), 8, "Version() should be an 8-char checksum prefix")
}

func TestCatalogData_Reason(t *testing.T) {
	t.Parallel()
	lib := newLibrary(t)

	tests := []struct {
		name       string
		m          Match
		wantReason bool // true => expect a non-empty Reason (suppressed)
	}{
		// --- credit cards (pii.credit_card, presidio + gitleaks) -------------
		{"visa test card", Match{"presidio", "pii.credit_card", "4111111111111111"}, true},
		{"visa test card, spaced", Match{"presidio", "pii.credit_card", "4111 1111 1111 1111"}, true},
		{"visa test card, dashed", Match{"presidio", "pii.credit_card", "4242-4242-4242-4242"}, true},
		{"mastercard test card", Match{"presidio", "pii.credit_card", "5555555555554444"}, true},
		{"amex test card (15)", Match{"presidio", "pii.credit_card", "378282246310005"}, true},
		{"discover test card", Match{"presidio", "pii.credit_card", "6011111111111117"}, true},
		{"diners test card (14)", Match{"presidio", "pii.credit_card", "30569309025904"}, true},
		{"jcb test card", Match{"presidio", "pii.credit_card", "3530111333300000"}, true},
		{"card from gitleaks source too", Match{"gitleaks", "pii.credit_card", "4242424242424242"}, true},
		// Luhn fallback: an unlisted but Luhn-valid 16-digit PAN in [13,19].
		{"unlisted luhn-valid card (fallback)", Match{"presidio", "pii.credit_card", "4539578763621486"}, true},
		// Real guards: a Luhn-INVALID card-shaped number is NOT suppressed, so we
		// are not blanket-dropping every 16-digit string.
		{"non-luhn card-shaped number survives", Match{"presidio", "pii.credit_card", "4111111111111112"}, false},
		{"non-luhn 16-digit survives", Match{"presidio", "pii.credit_card", "1234567890123456"}, false},
		// Scope guard: card digits on a non-credit_card rule are not suppressed
		// by the card rule.
		{"card digits on wrong rule survive", Match{"presidio", "pii.us_ssn", "4111111111111111"}, false},

		// --- Stripe test-mode keys (gitleaks, secret.stripe_*) ---------------
		{"stripe test secret key", Match{"gitleaks", "secret.stripe_access_token", "sk_test_4eC39HqLyjWDarjtT1zdp7dc"}, true},
		{"stripe test restricted key", Match{"gitleaks", "secret.stripe_access_token", "rk_test_deadbeefcafebabe0123"}, true},
		{"stripe test publishable key", Match{"gitleaks", "secret.stripe_access_token", "pk_test_TYooMQauvdEDq54NiTphI7jx"}, true},
		// Real guards.
		{"stripe LIVE key survives", Match{"gitleaks", "secret.stripe_access_token", "sk_live_51H8xExampleRealLiveKey"}, false},
		{"stripe test key on wrong source survives", Match{"custom", "secret.stripe_access_token", "sk_test_4eC39HqLyjWDarjtT1zdp7dc"}, false},
		{"stripe test key on wrong rule survives", Match{"gitleaks", "secret.aws_access_token", "sk_test_4eC39HqLyjWDarjtT1zdp7dc"}, false},

		// --- AWS example access key id (gitleaks, secret.aws_*) --------------
		{"aws example AKIA key id", Match{"gitleaks", "secret.aws_access_token", "AKIAIOSFODNN7EXAMPLE"}, true},
		{"aws example ASIA key id", Match{"gitleaks", "secret.aws_access_token", "ASIAIOSFODNN7EXAMPLE"}, true},
		// Real guard: a real-shaped AKIA key not ending EXAMPLE survives.
		{"real AKIA key survives", Match{"gitleaks", "secret.aws_access_token", "AKIAIOSFODNN7REALKEY"}, false},
		{"aws example on wrong rule survives", Match{"gitleaks", "secret.github_pat", "AKIAIOSFODNN7EXAMPLE"}, false},

		// --- AWS example secret access key (gitleaks, exact) -----------------
		{"aws example secret key", Match{"gitleaks", "secret.generic_api_key", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}, true},
		// Real guard: a similar-looking but different secret survives.
		{"real aws-shaped secret survives", Match{"gitleaks", "secret.generic_api_key", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYr3alKey12"}, false},

		// --- GitHub example token (gitleaks, secret.github_*) ----------------
		{"github docs example PAT", Match{"gitleaks", "secret.github_pat", "ghp_16C7e42F292c6912E7710c838347Ae178B4a"}, true},
		// Real guard: any other ghp_ token survives (exact match only).
		{"real github PAT survives", Match{"gitleaks", "secret.github_pat", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"}, false},

		// --- PostHog public project key (gitleaks) ---------------------------
		{"posthog public project key", Match{"gitleaks", "secret.generic_api_key", "phc_j9aBc0DeFgHiJkLmNoPqRsTuVwXyZ012345678"}, true},
		// Real guards.
		{"non-phc key survives", Match{"gitleaks", "secret.generic_api_key", "phx_notaposthogkey12345"}, false},
		{"phc_ from non-gitleaks source survives", Match{"presidio", "pii.email_address", "phc_j9aBc0DeFgHiJkLmNoPqRsTuVwXyZ012345678"}, false},

		// --- Sourcegraph bare git SHA (gitleaks, secret.sourcegraph_*) -------
		{"bare 40-hex git sha", Match{"gitleaks", "secret.sourcegraph_access_token", "da39a3ee5e6b4b0d3255bfef95601890afd80709"}, true},
		// Real guard: a real sgp_-prefixed token is NOT bare-40-hex, so survives.
		{"real sgp token survives", Match{"gitleaks", "secret.sourcegraph_access_token", "sgp_local_da39a3ee5e6b4b0d3255bfef95601890afd80709"}, false},
		// Scope guard: bare 40-hex on a different rule is not dropped by this rule.
		{"bare 40-hex on wrong rule survives", Match{"gitleaks", "secret.generic_api_key", "da39a3ee5e6b4b0d3255bfef95601890afd80709"}, false},

		// --- Go module hashes (gitleaks) -------------------------------------
		{"go.sum h1 hash", Match{"gitleaks", "secret.generic_api_key", "h1:Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8="}, true},
		// Real guard: a non-h1 secret survives.
		{"non-h1 secret survives", Match{"gitleaks", "secret.generic_api_key", "AbCd1234Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8Zt8="}, false},

		// --- placeholder / role emails (pii.email_address, email matcher) ----
		{"noreply on reserved fixture domain", Match{"presidio", "pii.email_address", "noreply@example.com"}, true},
		{"no-reply on acme fixture domain", Match{"presidio", "pii.email_address", "no-reply@acme.com"}, true},
		{"noreply on real-shaped domain (local-part path)", Match{"presidio", "pii.email_address", "noreply@mycompany.io"}, true},
		{"github ssh pseudo-user", Match{"presidio", "pii.email_address", "git@github.com"}, true},
		// Real guards: ordinary addresses are real PII and must survive.
		{"real gmail address survives", Match{"presidio", "pii.email_address", "alice@gmail.com"}, false},
		{"real corp address survives", Match{"presidio", "pii.email_address", "bob@internal.corp"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lib.Reason(tt.m)
			if tt.wantReason {
				require.NotEmptyf(t, got, "Reason(%+v) should be non-empty (suppressed FP)", tt.m)
			} else {
				require.Emptyf(t, got, "Reason(%+v) should be empty (real finding), got %q", tt.m, got)
			}
		})
	}
}
