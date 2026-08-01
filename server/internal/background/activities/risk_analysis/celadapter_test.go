package risk_analysis

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func celRules(t *testing.T, rules ...customrules.Rule) []CompiledCELRule {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	compiled, err := CompileCELRules(eng, rules)
	require.NoError(t, err)
	return compiled
}

func scanCEL(t *testing.T, view MessageView, rules []CompiledCELRule) []scanners.Finding {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	findings, err := ScanCELRules(eng, view, rules)
	require.NoError(t, err)
	return findings
}

// Correlated tool rule: both conditions bind to the same call; one finding per
// matched span.
func TestScanCELRules_CorrelatedToolRule(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID:        "custom.bash_drop",
		Title:         "bash drop table",
		Description:   "destructive SQL via a bash tool",
		DetectionExpr: `tool_calls.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchRegex("DROP TABLE"))`,
	})

	view := MessageView{
		Type:  message.ToolRequest,
		Tools: []ToolView{NewToolView("", "shell:run_bash_command", `{"command":"DROP TABLE users"}`)},
	}

	findings := scanCEL(t, view, rules)
	require.Len(t, findings, 2)

	byMatch := map[string]scanners.Finding{}
	for _, f := range findings {
		byMatch[f.Match] = f
		require.Equal(t, SourceCustom, f.Source)
		require.Equal(t, "custom.bash_drop", f.RuleID)
		require.Equal(t, "shell:run_bash_command", f.SpanGroupKey)
	}
	require.Contains(t, byMatch, "bash")
	require.Contains(t, byMatch, "DROP TABLE")
}

// Recorded call ids anchor findings per call: two calls to the same tool keep
// distinct group keys and carry their own call id for the reveal metadata.
func TestScanCELRules_SameToolTwiceGroupsPerCall(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID:        "custom.rm",
		DetectionExpr: `tool_calls.filter(t, t.args.get("command").matchRegex("rm -rf")).size() > 0`,
	})

	view := MessageView{
		Type: message.ToolRequest,
		Tools: []ToolView{
			NewToolView("call_1", "shell:run", `{"command":"rm -rf /tmp"}`),
			NewToolView("call_2", "shell:run", `{"command":"rm -rf /var"}`),
		},
	}

	findings := scanCEL(t, view, rules)
	require.Len(t, findings, 2)

	groupKeys := []string{findings[0].SpanGroupKey, findings[1].SpanGroupKey}
	callIDs := []string{findings[0].McpLookupToolCallID, findings[1].McpLookupToolCallID}
	require.ElementsMatch(t, []string{"call_1", "call_2"}, groupKeys)
	require.ElementsMatch(t, []string{"call_1", "call_2"}, callIDs)
}

// Without recorded ids the group key falls back to the tool name and no call
// id is claimed.
func TestScanCELRules_NoCallIDFallsBackToName(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID:        "custom.rm",
		DetectionExpr: `tool_calls.exists(t, t.args.get("command").matchRegex("rm -rf"))`,
	})

	view := MessageView{
		Type:  message.ToolRequest,
		Tools: []ToolView{NewToolView("", "shell:run", `{"command":"rm -rf /tmp"}`)},
	}

	findings := scanCEL(t, view, rules)
	require.Len(t, findings, 1)
	require.Equal(t, "shell:run", findings[0].SpanGroupKey)
	require.Empty(t, findings[0].McpLookupToolCallID)
}

// Correlation does not cross tools.
func TestScanCELRules_CorrelationDoesNotCrossTools(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID:        "custom.bash_drop",
		DetectionExpr: `tool_calls.exists(t, t.function.matchRegex("bash") && t.args.get("command").matchRegex("DROP TABLE"))`,
	})

	view := MessageView{
		Type: message.ToolRequest,
		Tools: []ToolView{
			NewToolView("", "shell:run_bash_command", `{"command":"ls"}`),
			NewToolView("", "db:query", `{"command":"DROP TABLE users"}`),
		},
	}

	require.Empty(t, scanCEL(t, view, rules))
}

// A content rule yields one finding per occurrence.
func TestScanCELRules_ContentRule(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID:        "custom.secret",
		DetectionExpr: `content.matchRegex("secret")`,
	})

	findings := scanCEL(t, MessageView{Type: message.User, Content: "the secret is a secret"}, rules)
	require.Len(t, findings, 2)
	require.Equal(t, 4, findings[0].StartPos)
	require.Equal(t, 16, findings[1].StartPos)
}

// Legacy regex rules (no detection_expr) evaluate as content.matchRegex(regex).
func TestScanCELRules_LegacyRegexFallback(t *testing.T) {
	t.Parallel()
	rules := celRules(t, customrules.Rule{
		RuleID: "custom.legacy",
		Regex:  "AKIA[0-9A-Z]{16}",
	})

	findings := scanCEL(t, MessageView{Type: message.User, Content: "key AKIA1234567890ABCDEF here"}, rules)
	require.Len(t, findings, 1)
	require.Equal(t, "AKIA1234567890ABCDEF", findings[0].Match)
}
