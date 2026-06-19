package categories

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassify_PinsPriorCASEBehavior(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		ruleID string
		want   Category
	}{
		// Source-based categories take precedence over rule_id.
		{name: "shadow_mcp source wins", source: "shadow_mcp", ruleID: "anything", want: CategoryShadowMCP},
		{name: "destructive_tool source", source: "destructive_tool", ruleID: "", want: CategoryDestructiveTool},
		{name: "cli_destructive source", source: "cli_destructive", ruleID: "secret.foo", want: CategoryCLIDestructive},
		{name: "prompt_injection source", source: "prompt_injection", ruleID: "", want: CategoryPromptInjection},
		{name: "llm_judge source", source: "llm_judge", ruleID: "llm_judge", want: CategoryPromptPolicy},

		// Secrets prefix.
		{name: "secret aws", source: "gitleaks", ruleID: "secret.aws_access_key", want: CategorySecrets},
		{name: "secret jwt", source: "gitleaks", ruleID: "secret.jwt", want: CategorySecrets},

		// Financial — explicit list beats generic pii.% prefix.
		{name: "pii credit card", source: "presidio", ruleID: "pii.credit_card", want: CategoryFinancial},
		{name: "pii iban", source: "presidio", ruleID: "pii.iban_code", want: CategoryFinancial},

		// Government IDs.
		{name: "pii us ssn", source: "presidio", ruleID: "pii.us_ssn", want: CategoryGovernmentIDs},
		{name: "pii in aadhaar", source: "presidio", ruleID: "pii.in_aadhaar", want: CategoryGovernmentIDs},

		// Healthcare.
		{name: "pii medical license", source: "presidio", ruleID: "pii.medical_license", want: CategoryHealthcare},
		{name: "pii medical disease", source: "presidio", ruleID: "pii.medical_disease_disorder", want: CategoryHealthcare},

		// Off-policy.
		{name: "pii harmful", source: "presidio", ruleID: "pii.harmful_content_request", want: CategoryOffPolicy},
		{name: "pii topic boundary", source: "presidio", ruleID: "pii.topic_boundary_violation", want: CategoryOffPolicy},

		// PII catch-all.
		{name: "pii email", source: "presidio", ruleID: "pii.email_address", want: CategoryPII},
		{name: "pii phone", source: "presidio", ruleID: "pii.phone_number", want: CategoryPII},
		{name: "pii ip", source: "presidio", ruleID: "pii.ip_address", want: CategoryPII},

		// Scanner-source fallbacks: when the rule_id doesn't carry a prefix,
		// we still resolve to the user-facing category instead of exposing
		// the scanner name to the UI.
		{name: "gitleaks bare", source: "gitleaks", ruleID: "generic-api-key", want: CategorySecrets},
		{name: "gitleaks empty rule", source: "gitleaks", ruleID: "", want: CategorySecrets},
		{name: "presidio bare", source: "presidio", ruleID: "email", want: CategoryPII},

		// Custom fallback.
		{name: "empty", source: "", ruleID: "", want: CategoryCustom},
		{name: "unknown", source: "custom_regex", ruleID: "company.foobar", want: CategoryCustom},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, Classify(tc.source, tc.ruleID))
		})
	}
}

func TestFilterFor(t *testing.T) {
	t.Parallel()

	t.Run("source category", func(t *testing.T) {
		t.Parallel()
		f := FilterFor(CategoryShadowMCP)
		require.Equal(t, []string{"shadow_mcp"}, f.Sources)
		require.Empty(t, f.RuleIDs)
		require.Empty(t, f.RulePrefix)
	})

	t.Run("prefix category", func(t *testing.T) {
		t.Parallel()
		f := FilterFor(CategorySecrets)
		require.Equal(t, "secret.", f.RulePrefix)
		require.Empty(t, f.Sources)
		require.Empty(t, f.RuleIDs)
	})

	t.Run("explicit-list category", func(t *testing.T) {
		t.Parallel()
		f := FilterFor(CategoryFinancial)
		require.Contains(t, f.RuleIDs, "pii.credit_card")
		require.Empty(t, f.RulePrefix)
	})

	t.Run("custom is no-op", func(t *testing.T) {
		t.Parallel()
		f := FilterFor(CategoryCustom)
		require.Empty(t, f.Sources)
		require.Empty(t, f.RuleIDs)
		require.Empty(t, f.RulePrefix)
	})

	t.Run("empty is no-op", func(t *testing.T) {
		t.Parallel()
		f := FilterFor("")
		require.Empty(t, f.Sources)
		require.Empty(t, f.RuleIDs)
		require.Empty(t, f.RulePrefix)
	})
}

func TestAll_IncludesCustom(t *testing.T) {
	t.Parallel()
	all := All()
	require.Equal(t, CategoryCustom, all[len(all)-1].Category)
}
