package risk_analysis

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
)

func mustJSON(t *testing.T, cfg MatchConfig) []byte {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return raw
}

// --- serializer ---------------------------------------------------------------

func TestMatchConfigToCEL_TextRegex(t *testing.T) {
	t.Parallel()
	expr, err := MatchConfigToCEL(MatchConfig{
		Combine:    CombineAnd,
		Conditions: []Condition{{Target: TargetContent, Op: OpRegex, Value: "secret"}},
	})
	require.NoError(t, err)
	require.Equal(t, `content.match("secret")`, expr)
}

func TestMatchConfigToCEL_ToolConditionsAreCorrelated(t *testing.T) {
	t.Parallel()
	// two tool conditions => one tools.exists so they bind to the SAME call.
	expr, err := MatchConfigToCEL(MatchConfig{
		Combine: CombineAnd,
		Conditions: []Condition{
			{Target: TargetToolFunction, Op: OpRegex, Value: "bash"},
			{Target: TargetToolArgs, Op: OpRegex, Value: "DROP TABLE", Path: "command"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, `tools.exists(t, t.function.match("bash") && t.args.get("command").match("DROP TABLE"))`, expr)
}

func TestMatchConfigToCEL_MixedTargets(t *testing.T) {
	t.Parallel()
	expr, err := MatchConfigToCEL(MatchConfig{
		Combine: CombineOr,
		Conditions: []Condition{
			{Target: TargetContent, Op: OpContains, Values: []string{"a", "b"}},
			{Target: TargetToolServer, Op: OpEquals, Value: ""},
		},
	})
	require.NoError(t, err)
	require.Equal(t, `(content.includes("a") || content.includes("b")) || tools.exists(t, t.server.eq(""))`, expr)
}

// --- end-to-end: structured rule -> CEL -> Findings against a real view -------

func newCELEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func TestScanCELRules_CorrelatedToolRule(t *testing.T) {
	t.Parallel()
	eng := newCELEngine(t)

	rules, err := CompileCELRules(eng, []CustomDetectionRule{{
		RuleID:      "custom.bash_drop",
		Title:       "bash drop table",
		Description: "destructive SQL via a bash tool",
		MatchConfig: mustJSON(t, MatchConfig{
			Combine: CombineAnd,
			Conditions: []Condition{
				{Target: TargetToolFunction, Op: OpRegex, Value: "bash"},
				{Target: TargetToolArgs, Op: OpRegex, Value: "DROP TABLE", Path: "command"},
			},
		}),
		Action: ActionDeny,
	}})
	require.NoError(t, err)
	require.Len(t, rules, 1)

	// view built like customRuleMessageView would for a tool_request.
	view := MessageView{
		Type: message.ToolRequest,
		Tools: []ToolView{
			NewToolView("shell:run_bash_command", `{"command":"DROP TABLE users"}`),
		},
	}

	findings, err := ScanCELRules(eng, view, rules)
	require.NoError(t, err)
	require.Len(t, findings, 2) // one span per matched condition

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

func TestScanCELRules_CorrelationDoesNotCrossTools(t *testing.T) {
	t.Parallel()
	eng := newCELEngine(t)

	rules, err := CompileCELRules(eng, []CustomDetectionRule{{
		RuleID: "custom.bash_drop",
		MatchConfig: mustJSON(t, MatchConfig{
			Combine: CombineAnd,
			Conditions: []Condition{
				{Target: TargetToolFunction, Op: OpRegex, Value: "bash"},
				{Target: TargetToolArgs, Op: OpRegex, Value: "DROP TABLE", Path: "command"},
			},
		}),
		Action: ActionDeny,
	}})
	require.NoError(t, err)

	// bash tool is harmless; the DROP is in a different (non-bash) tool.
	view := MessageView{
		Type: message.ToolRequest,
		Tools: []ToolView{
			NewToolView("shell:run_bash_command", `{"command":"ls"}`),
			NewToolView("db:query", `{"command":"DROP TABLE users"}`),
		},
	}

	findings, err := ScanCELRules(eng, view, rules)
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestScanCELRules_ContentRule(t *testing.T) {
	t.Parallel()
	eng := newCELEngine(t)

	rules, err := CompileCELRules(eng, []CustomDetectionRule{{
		RuleID: "custom.secret",
		MatchConfig: mustJSON(t, MatchConfig{
			Combine:    CombineAnd,
			Conditions: []Condition{{Target: TargetContent, Op: OpRegex, Value: "secret"}},
		}),
		Action: ActionDeny,
	}})
	require.NoError(t, err)

	view := MessageView{Type: message.User, Content: "the secret is a secret"}
	findings, err := ScanCELRules(eng, view, rules)
	require.NoError(t, err)
	require.Len(t, findings, 2)
	require.Equal(t, 4, findings[0].StartPos)
	require.Equal(t, 16, findings[1].StartPos)
}
