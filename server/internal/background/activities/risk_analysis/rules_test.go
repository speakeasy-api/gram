package risk_analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestCanonicalPresidioRuleID_SnakeCasesAndPrependsPII(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "pii.medical_license", CanonicalPresidioRuleID("MEDICAL_LICENSE"))
	assert.Equal(t, "pii.credit_card", CanonicalPresidioRuleID("CREDIT_CARD"))
	assert.Equal(t, "pii.us_ssn", CanonicalPresidioRuleID("US_SSN"))
	assert.Equal(t, "pii.email_address", CanonicalPresidioRuleID("EMAIL_ADDRESS"))
}

func TestValidateRuleID_AcceptsCanonicalForms(t *testing.T) {
	t.Parallel()

	valid := []string{
		"shadow_mcp",
		"secret.anthropic_api_key",
		"pii.credit_card",
		"pii.medical_license",
		"destructive.tool",
		"destructive.shell.rm_rf",
		"destructive.cloud.kubectl_delete_namespace",
		"prompt_injection",
	}
	for _, id := range valid {
		assert.NoError(t, scanners.ValidateRuleID(id), "expected %q to validate", id)
	}
}

func TestValidateRuleID_RejectsMalformed(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",                     // empty
		"UPPER_SNAKE",          // uppercase
		"kebab-case",           // hyphen
		"shell/rm_rf",          // slash
		"trailing_dot.",        // empty trailing segment
		".leading_dot",         // empty leading segment
		"double..dot",          // empty middle segment
		"trailing_underscore_", // empty trailing snake piece
		"_leading_underscore",  // empty leading snake piece
		"double__underscore",   // empty middle snake piece
		"spaces in id",         // spaces
		"Mixed.Case",           // uppercase
	}
	for _, id := range invalid {
		assert.Error(t, scanners.ValidateRuleID(id), "expected %q to fail validation", id)
	}
}

// TestDescribe_AllBuildersReturnCanonicalIDs locks in the contract that
// every Describe* builder hands back an id matching the canonical grammar.
// guard() in rules.go already panics in dev/test on a mismatch — calling
// each builder here is the integration check that pins it.
func TestDescribe_AllBuildersReturnCanonicalIDs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		ruleID string
		fn     func() (string, string)
	}{
		{"pii.credit_card", "pii.credit_card", func() (string, string) { return DescribePresidioEntity("CREDIT_CARD") }},
		{"pii.dead_letter", "pii.dead_letter", DescribePresidioDeadLetter},
		{"prompt_injection", "prompt_injection", DescribePromptInjection},
	}
	for _, c := range cases {
		id, desc := c.fn()
		assert.Equal(t, c.ruleID, id, c.name)
		assert.NotEmpty(t, desc, c.name)
		assert.NoError(t, scanners.ValidateRuleID(id), c.name)
	}
}

func TestDescribePresidioEntity_UsesCatalogedDescription(t *testing.T) {
	t.Parallel()

	id, desc := DescribePresidioEntity("MEDICAL_LICENSE")
	assert.Equal(t, "pii.medical_license", id)
	assert.Equal(t, "Identified a medical license number, which may expose protected health information.", desc)
	assert.NotContains(t, desc, "MEDICAL_LICENSE", "description must not echo the rule id")
}

func TestDescribePresidioEntity_UnknownEntityFallsBackToGeneric(t *testing.T) {
	t.Parallel()

	id, desc := DescribePresidioEntity("UNKNOWN_ENTITY")
	assert.Equal(t, "pii.unknown_entity", id)
	assert.Equal(t, "Identified potentially sensitive personal information.", desc)
}

func TestDescribeBuilders_NeverLeakSensitiveMatch(t *testing.T) {
	t.Parallel()

	// Sentinel values that resemble PII / secrets / attack phrases. The
	// content scanners must never produce descriptions that echo them.
	cases := []struct {
		name     string
		desc     string
		sentinel string
	}{
		{
			name: "presidio email",
			desc: func() string {
				_, d := DescribePresidioEntity("EMAIL_ADDRESS")
				return d
			}(),
			sentinel: "alice@example.com",
		},
		{
			name: "presidio medical",
			desc: func() string {
				_, d := DescribePresidioEntity("MEDICAL_LICENSE")
				return d
			}(),
			sentinel: "real-medical-license-12345",
		},
		{
			name: "prompt injection",
			desc: func() string {
				_, d := DescribePromptInjection()
				return d
			}(),
			sentinel: "ignore previous instructions",
		},
	}
	for _, c := range cases {
		assert.NotContains(t, strings.ToLower(c.desc), strings.ToLower(c.sentinel),
			"%s: description leaked sensitive sentinel", c.name)
	}
}
