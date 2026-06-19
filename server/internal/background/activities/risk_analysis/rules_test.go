package risk_analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalGitleaksRuleID_PrependsSecret(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "secret.anthropic_api_key", CanonicalGitleaksRuleID("anthropic-api-key"))
	assert.Equal(t, "secret.aws_access_token", CanonicalGitleaksRuleID("AWS-Access-Token"))
}

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
		assert.NoError(t, ValidateRuleID(id), "expected %q to validate", id)
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
		assert.Error(t, ValidateRuleID(id), "expected %q to fail validation", id)
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
		{"shadow_mcp (no tool)", "shadow_mcp", func() (string, string) { return DescribeShadowMCP("") }},
		{"shadow_mcp (with tool)", "shadow_mcp", func() (string, string) { return DescribeShadowMCP("mcp__db__delete") }},
		{"destructive.tool", "destructive.tool", func() (string, string) { return DescribeDestructiveTool("delete_records") }},
		{"destructive.shell.rm_rf", "destructive.shell.rm_rf", func() (string, string) {
			return DescribeCLIDestructive(cliDestructivePattern{Category: "shell", Name: "rm_rf"}, "Bash")
		}},
		{"pii.credit_card", "pii.credit_card", func() (string, string) { return DescribePresidioEntity("CREDIT_CARD") }},
		{"pii.dead_letter", "pii.dead_letter", DescribePresidioDeadLetter},
		{"secret.anthropic_api_key", "secret.anthropic_api_key", func() (string, string) {
			return DescribeGitleaks("anthropic-api-key", "Identified an Anthropic API Key.")
		}},
		{"prompt_injection", "prompt_injection", DescribePromptInjection},
	}
	for _, c := range cases {
		id, desc := c.fn()
		assert.Equal(t, c.ruleID, id, c.name)
		assert.NotEmpty(t, desc, c.name)
		assert.NoError(t, ValidateRuleID(id), c.name)
	}
}

func TestDescribeShadowMCP_DescriptionIncludesToolName(t *testing.T) {
	t.Parallel()

	_, desc := DescribeShadowMCP("mcp__db__delete")
	assert.Contains(t, desc, "mcp__db__delete")
	assert.NotContains(t, desc, "x-gram-toolset-id", "must not leak validator internals")
}

func TestDescribeDestructiveTool_DescriptionIncludesToolName(t *testing.T) {
	t.Parallel()

	_, desc := DescribeDestructiveTool("delete_records")
	assert.Contains(t, desc, "delete_records")
	assert.Contains(t, desc, "destructive")
}

func TestDescribeCLIDestructive_IncludesToolAndCommand(t *testing.T) {
	t.Parallel()

	_, desc := DescribeCLIDestructive(cliDestructivePattern{Category: "shell", Name: "rm_rf"}, "Bash")
	assert.Contains(t, desc, "Bash", "description must include the tool name")
	assert.Contains(t, desc, "rm -rf", "description must include the human-readable command")
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

func TestDescribeGitleaks_PassesThroughUpstreamDescription(t *testing.T) {
	t.Parallel()

	id, desc := DescribeGitleaks("some-new-gitleaks-rule", "Identified a Foo API key.")
	assert.Equal(t, "secret.some_new_gitleaks_rule", id)
	assert.Equal(t, "Identified a Foo API key.", desc)
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

// TestCLIDestructivePattern_FullNameProducesCanonicalRuleID verifies the
// pattern type emits a canonical rule id directly, with no indirection
// layer in rules.go.
func TestCLIDestructivePattern_FullNameProducesCanonicalRuleID(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "destructive.shell.rm_rf", (cliDestructivePattern{Category: "shell", Name: "rm_rf"}).FullName())
	assert.Equal(t, "destructive.git.push_force", (cliDestructivePattern{Category: "git", Name: "push_force"}).FullName())
	assert.Equal(t, "destructive.database.drop", (cliDestructivePattern{Category: "database", Name: "drop"}).FullName())
	assert.Equal(t, "destructive.cloud.kubectl_delete_namespace", (cliDestructivePattern{Category: "cloud", Name: "kubectl_delete_namespace"}).FullName())
}

func TestGuard_PanicsOnMalformedRuleIDInTest(t *testing.T) {
	t.Parallel()

	// testing.Testing() is true here so enforceRuleIDFormat is on. Wrap a
	// known-bad id and assert it panics.
	require.Panics(t, func() {
		guard("UPPER_SNAKE_INVALID")
	})
}
