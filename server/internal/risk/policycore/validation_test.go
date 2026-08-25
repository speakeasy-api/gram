package policycore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestValidateActionAndSourceCompatibility(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"flag", "warn", "block", "quarantine"} {
		require.NoError(t, ValidateAction(action))
	}
	require.EqualError(t, ValidateAction("deny"), "action must be one of: flag, warn, block, quarantine")

	require.NoError(t, ValidateSources([]string{ra.SourceGitleaks, ra.SourcePresidio, shadowmcp.SourceShadowMCP}))
	require.EqualError(t, ValidateSources([]string{"unknown"}), `source "unknown" is not a recognized policy source`)

	for _, action := range []string{"warn", "block", "quarantine"} {
		require.Error(t, ValidateSourceAction([]string{shadowmcp.SourceDestructiveTool}, action))
		require.Error(t, ValidateSourceAction([]string{ra.SourceCLIDestructive}, action))
		require.Error(t, ValidateSourceAction([]string{ra.SourceAccountIdentity}, action))
		require.NoError(t, ValidateSourceAction([]string{ra.SourceGitleaks}, action))
	}
}

func TestNormalizeApprovedEmailDomains(t *testing.T) {
	t.Parallel()

	got, err := NormalizeApprovedEmailDomains([]string{" Example.COM ", "@example.com", "", "sub.example.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com", "sub.example.com"}, got)

	_, err = NormalizeApprovedEmailDomains([]string{"not-a-domain"})
	require.EqualError(t, err, `approved email domain "not-a-domain" is not a valid domain`)
}

func TestValidatePolicyFields(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateName("Policy"))
	require.EqualError(t, ValidateName(""), "name must not be empty")
	require.EqualError(t, ValidateName(strings.Repeat("x", 101)), "name must be at most 100 characters")

	require.NoError(t, ValidatePolicyType(ra.PolicyTypeStandard))
	require.NoError(t, ValidatePolicyType(ra.PolicyTypePromptBased))
	require.EqualError(t, ValidatePolicyType("other"), "policy_type must be one of: standard, prompt_based")

	require.NoError(t, ValidateCustomRuleIDs([]string{"custom.rule"}))
	require.Error(t, ValidateCustomRuleIDs([]string{"builtin.rule"}))
	require.NoError(t, ValidateMessageTypes(nil))
	require.Error(t, ValidateMessageTypes([]string{"unknown"}))
}

func TestValidateDetectionScopes(t *testing.T) {
	t.Parallel()

	eng, err := celenv.New()
	require.NoError(t, err)
	include := ` kind == "user_message" `

	got, err := ValidateDetectionScopes(eng, []*DetectionScopeInput{{
		Category:     "prompt_injection",
		ScopeInclude: &include,
	}})
	require.NoError(t, err)
	require.Equal(t, []ra.DetectionScopeConfig{{
		Category:     "prompt_injection",
		ScopeInclude: `kind == "user_message"`,
		ScopeExempt:  "",
	}}, got)

	invalidCEL := "kind ="
	_, err = ValidateDetectionScopes(eng, []*DetectionScopeInput{{
		Category:     "secrets",
		ScopeInclude: &invalidCEL,
	}})
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)
	require.Equal(t, `detection scope for "secrets" does not compile`, validationErr.Message)
	require.Error(t, validationErr.Cause)

	invalid := []struct {
		name   string
		scopes []*DetectionScopeInput
	}{
		{name: "null", scopes: []*DetectionScopeInput{nil}},
		{name: "unknown", scopes: []*DetectionScopeInput{{Category: "unknown"}}},
		{name: "session scoped", scopes: []*DetectionScopeInput{{Category: "account_identity"}}},
		{name: "duplicate", scopes: []*DetectionScopeInput{{Category: "secrets"}, {Category: "secrets"}}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateDetectionScopes(eng, test.scopes)
			require.Error(t, err)
		})
	}
}
