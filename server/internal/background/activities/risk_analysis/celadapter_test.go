package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
)

func celRules(t *testing.T, rules ...CustomDetectionRule) []CompiledCELRule {
	t.Helper()
	eng, err := CELEngine()
	require.NoError(t, err)
	compiled, err := CompileCELRules(eng, rules)
	require.NoError(t, err)
	return compiled
}

func scanCEL(t *testing.T, view MessageView, rules []CompiledCELRule) []Finding {
	t.Helper()
	eng, err := CELEngine()
	require.NoError(t, err)
	findings, err := ScanCELRules(eng, view, rules)
	require.NoError(t, err)
	return findings
}

// Correlated tool rule: both conditions bind to the same call; one finding per
// matched span.
func TestScanCELRules_CorrelatedToolRule(t *testing.T) {
	t.Parallel()
	rules := celRules(t, CustomDetectionRule{
		RuleID:       "custom.bash_drop",
		Title:        "bash drop table",
		Description:  "destructive SQL via a bash tool",
		DetectionCel: `tools.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchRegex("DROP TABLE"))`,
	})

	view := MessageView{
		Type:  message.ToolRequest,
		Tools: []ToolView{NewToolView("shell:run_bash_command", `{"command":"DROP TABLE users"}`)},
	}

	findings := scanCEL(t, view, rules)
	require.Len(t, findings, 2)

	byMatch := map[string]Finding{}
	for _, f := range findings {
		byMatch[f.Match] = f
		require.Equal(t, SourceCustom, f.Source)
		require.Equal(t, "custom.bash_drop", f.RuleID)
		require.Equal(t, "shell:run_bash_command", f.toolCallID)
	}
	require.Contains(t, byMatch, "bash")
	require.Contains(t, byMatch, "DROP TABLE")
}

// Correlation does not cross tools.
func TestScanCELRules_CorrelationDoesNotCrossTools(t *testing.T) {
	t.Parallel()
	rules := celRules(t, CustomDetectionRule{
		RuleID:       "custom.bash_drop",
		DetectionCel: `tools.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchRegex("DROP TABLE"))`,
	})

	view := MessageView{
		Type: message.ToolRequest,
		Tools: []ToolView{
			NewToolView("shell:run_bash_command", `{"command":"ls"}`),
			NewToolView("db:query", `{"command":"DROP TABLE users"}`),
		},
	}

	require.Empty(t, scanCEL(t, view, rules))
}

// A content rule yields one finding per occurrence.
func TestScanCELRules_ContentRule(t *testing.T) {
	t.Parallel()
	rules := celRules(t, CustomDetectionRule{
		RuleID:       "custom.secret",
		DetectionCel: `content.matchRegex("secret")`,
	})

	findings := scanCEL(t, MessageView{Type: message.User, Content: "the secret is a secret"}, rules)
	require.Len(t, findings, 2)
	require.Equal(t, 4, findings[0].StartPos)
	require.Equal(t, 16, findings[1].StartPos)
}

// Legacy regex rules (no detection_cel) evaluate as content.matchRegex(regex).
func TestScanCELRules_LegacyRegexFallback(t *testing.T) {
	t.Parallel()
	rules := celRules(t, CustomDetectionRule{
		RuleID: "custom.legacy",
		Regex:  "AKIA[0-9A-Z]{16}",
	})

	findings := scanCEL(t, MessageView{Type: message.User, Content: "key AKIA1234567890ABCDEF here"}, rules)
	require.Len(t, findings, 1)
	require.Equal(t, "AKIA1234567890ABCDEF", findings[0].Match)
}
