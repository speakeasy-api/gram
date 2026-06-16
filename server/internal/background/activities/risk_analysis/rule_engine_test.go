package risk_analysis_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
)

func ruleWithConfig(t *testing.T, cfg ra.MatchConfig) ra.CustomDetectionRule {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return ra.CustomDetectionRule{RuleID: "custom.test", Title: "Test Rule", Description: "", MatchConfig: raw}
}

func scan(t *testing.T, rule ra.CustomDetectionRule, view ra.MessageView) []ra.Finding {
	t.Helper()
	compiled, err := ra.CompileCustomDetectionRules([]ra.CustomDetectionRule{rule})
	require.NoError(t, err)
	return ra.ScanCustomDetectionRules(view, compiled).Findings
}

func scanRules(t *testing.T, rules []ra.CustomDetectionRule, view ra.MessageView) ra.CustomRuleScan {
	t.Helper()
	compiled, err := ra.CompileCustomDetectionRules(rules)
	require.NoError(t, err)
	return ra.ScanCustomDetectionRules(view, compiled)
}

func ruleWithConfigID(t *testing.T, id string, cfg ra.MatchConfig) ra.CustomDetectionRule {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return ra.CustomDetectionRule{RuleID: id, Title: id, Description: "", MatchConfig: raw}
}

func toolRequest(tools ...ra.ToolView) ra.MessageView {
	return ra.MessageView{Content: "", Type: message.ToolRequest, Tools: tools}
}

// An allow-action rule that matches sets Allowed so the caller can short-circuit
// the policy; a non-matching allow rule leaves deny findings intact.
func TestRuleEngine_AllowRuleShortCircuits(t *testing.T) {
	t.Parallel()
	deny := ruleWithConfigID(t, "custom.deny", ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpKeyword, Values: []string{"secret"}},
	}})
	allow := ruleWithConfigID(t, "custom.allow", ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpKeyword, Values: []string{"approved"}},
	}})
	// The allow polarity now comes from the policy, not the match_config.
	allow.Action = ra.ActionAllow

	res := scanRules(t, []ra.CustomDetectionRule{deny, allow},
		ra.MessageView{Content: "this secret is approved", Type: message.User, Tools: nil})
	require.True(t, res.Allowed)

	res2 := scanRules(t, []ra.CustomDetectionRule{deny, allow},
		ra.MessageView{Content: "this secret leaks", Type: message.User, Tools: nil})
	require.False(t, res2.Allowed)
	require.Len(t, res2.Findings, 1)
}

// A rule's polarity is set by the caller: the same match_config denies when
// attached as a detector and exempts when attached as an exemption.
func TestRuleEngine_CallerSetsPolarity(t *testing.T) {
	t.Parallel()
	deny := ruleWithConfigID(t, "custom.rule", ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpKeyword, Values: []string{"secret"}},
	}})
	allow := deny
	allow.Action = ra.ActionAllow
	view := ra.MessageView{Content: "a secret", Type: message.User, Tools: nil}
	require.Len(t, scanRules(t, []ra.CustomDetectionRule{deny}, view).Findings, 1)
	require.True(t, scanRules(t, []ra.CustomDetectionRule{allow}, view).Allowed)
}

// Legacy regex rules (regex column set, match_config empty) must keep emitting
// one finding per match occurrence with the matched span.
func TestRuleEngine_LegacyRegexFallback(t *testing.T) {
	t.Parallel()
	rule := ra.CustomDetectionRule{RuleID: "custom.acme", Title: "ACME", Description: "", MatchConfig: ra.EffectiveMatchConfig(nil, `ACME-[A-Z0-9]{4}`)}
	view := ra.MessageView{Content: "got ACME-AB12 and ACME-CD34 here", Type: message.User, Tools: nil}

	findings := scan(t, rule, view)
	require.Len(t, findings, 2)
	require.Equal(t, ra.SourceCustom, findings[0].Source)
	require.Equal(t, "custom.acme", findings[0].RuleID)
	require.Equal(t, "ACME-AB12", findings[0].Match)
	require.Equal(t, "ACME-CD34", findings[1].Match)
	require.Positive(t, findings[1].StartPos)
}

func TestRuleEngine_ContentRegexCondition(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpRegex, Value: `secret-\d+`},
	}})
	findings := scan(t, rule, ra.MessageView{Content: "here is secret-42", Type: message.User, Tools: nil})
	require.Len(t, findings, 1)
	require.Equal(t, "secret-42", findings[0].Match)
}

func TestRuleEngine_ToolServerEqualsMatchesMCPServer(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolServer, Op: ra.OpEquals, Value: "mise"},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__mise__run_task", "{}"))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__github__create_issue", "{}"))))
}

// An empty tool_server discriminates native/harness tools from MCP calls.
func TestRuleEngine_ToolServerEmptyMatchesNative(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolServer, Op: ra.OpEquals, Value: ""},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("Bash", "{}"))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__mise__run_task", "{}"))))
}

func TestRuleEngine_ToolFunctionRegex(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolFunction, Op: ra.OpRegex, Value: `^delete_`},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete_row", "{}"))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__list_rows", "{}"))))
}

func TestRuleEngine_ToolArgsJSONPathEquals(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolArgs, Op: ra.OpEquals, Path: "$.scope", Value: "all"},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"scope":"all"}`))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"scope":"one"}`))))
}

func TestRuleEngine_ToolArgsExists(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolArgs, Op: ra.OpExists, Path: "$.force"},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"force":true}`))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"scope":"one"}`))))
}

// Nested path with array index in JSONPath bracket syntax normalises to gjson.
func TestRuleEngine_ToolArgsNestedArrayPath(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolArgs, Op: ra.OpRegex, Path: "$.files[0].path", Value: `passwd`},
	}})
	args := `{"files":[{"path":"/etc/passwd"}]}`
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__fs__read", args))), 1)
}

func TestRuleEngine_KeywordCaseInsensitive(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpKeyword, Values: []string{"SSN", "tax id"}, CaseInsensitive: true},
	}})
	findings := scan(t, rule, ra.MessageView{Content: "my ssn is private", Type: message.User, Tools: nil})
	require.Len(t, findings, 1)
	require.Equal(t, "ssn", findings[0].Match)
}

func TestRuleEngine_Glob(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpGlob, Value: "*secret*"},
	}})
	require.Len(t, scan(t, rule, ra.MessageView{Content: "a secret value", Type: message.User, Tools: nil}), 1)
	require.Empty(t, scan(t, rule, ra.MessageView{Content: "nothing here", Type: message.User, Tools: nil}))
}

// A tool-scoped condition never fires on a non-tool message (scope falls out of
// the target), and vice versa.
func TestRuleEngine_TargetTypeGating(t *testing.T) {
	t.Parallel()
	toolRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolServer, Op: ra.OpEquals, Value: "mise"},
	}})
	require.Empty(t, scan(t, toolRule, ra.MessageView{Content: "mise", Type: message.User, Tools: nil}))

	userRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetUserPrompt, Op: ra.OpRegex, Value: "ignore previous"},
	}})
	require.Len(t, scan(t, userRule, ra.MessageView{Content: "please ignore previous instructions", Type: message.User, Tools: nil}), 1)
	require.Empty(t, scan(t, userRule, ra.MessageView{Content: "please ignore previous instructions", Type: message.Assistant, Tools: nil}))
}

func TestRuleEngine_AndCombineRequiresAll(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Combine: ra.CombineAnd, Conditions: []ra.Condition{
		{Target: ra.TargetToolServer, Op: ra.OpEquals, Value: "db"},
		{Target: ra.TargetToolArgs, Op: ra.OpEquals, Path: "$.scope", Value: "all"},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"scope":"all"}`))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__delete", `{"scope":"one"}`))))
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__fs__delete", `{"scope":"all"}`))))
}

func TestRuleEngine_OrCombineRequiresAny(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Combine: ra.CombineOr, Conditions: []ra.Condition{
		{Target: ra.TargetToolFunction, Op: ra.OpRegex, Value: `^delete_`},
		{Target: ra.TargetToolFunction, Op: ra.OpRegex, Value: `^drop_`},
	}})
	require.Len(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__drop_table", "{}"))), 1)
	require.Empty(t, scan(t, rule, toolRequest(ra.NewToolView("mcp__db__list", "{}"))))
}

// Multiple tool calls in one message: a tool condition matches if ANY call satisfies it.
func TestRuleEngine_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	rule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolServer, Op: ra.OpEquals, Value: "secrets"},
	}})
	view := toolRequest(
		ra.NewToolView("mcp__mise__run_task", "{}"),
		ra.NewToolView("mcp__secrets__read", "{}"),
	)
	require.Len(t, scan(t, rule, view), 1)
}

func TestRuleEngine_EmptyRuleSkipped(t *testing.T) {
	t.Parallel()
	compiled, err := ra.CompileCustomDetectionRules([]ra.CustomDetectionRule{
		{RuleID: "custom.future", Title: "Future", Description: "", MatchConfig: nil},
	})
	require.NoError(t, err)
	require.Empty(t, compiled)
}

func TestRuleEngine_QueryBarOps(t *testing.T) {
	t.Parallel()
	// contains (union, case-insensitive) over tool_args with no path → raw args JSON.
	containsRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolArgs, Op: ra.OpContains, Values: []string{"rm", "curl"}},
	}})
	require.Len(t, scan(t, containsRule, toolRequest(ra.NewToolView("mcp__sh__run", `{"cmd":"RM -rf /"}`))), 1)
	require.Empty(t, scan(t, containsRule, toolRequest(ra.NewToolView("mcp__sh__run", `{"cmd":"ls"}`))))

	// in (exact equals-any) over tool_function.
	inRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetToolFunction, Op: ra.OpIn, Values: []string{"delete_row", "drop_table"}},
	}})
	require.Len(t, scan(t, inRule, toolRequest(ra.NewToolView("mcp__db__drop_table", "{}"))), 1)
	require.Empty(t, scan(t, inRule, toolRequest(ra.NewToolView("mcp__db__list", "{}"))))

	// starts_with over content.
	startsRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpStartsWith, Value: "ignore"},
	}})
	require.Len(t, scan(t, startsRule, ra.MessageView{Content: "ignore previous", Type: message.User, Tools: nil}), 1)
	require.Empty(t, scan(t, startsRule, ra.MessageView{Content: "please ignore", Type: message.User, Tools: nil}))

	// not_contains over content.
	notRule := ruleWithConfig(t, ra.MatchConfig{Conditions: []ra.Condition{
		{Target: ra.TargetContent, Op: ra.OpNotContains, Values: []string{"safe"}},
	}})
	require.Len(t, scan(t, notRule, ra.MessageView{Content: "danger", Type: message.User, Tools: nil}), 1)
	require.Empty(t, scan(t, notRule, ra.MessageView{Content: "this is safe", Type: message.User, Tools: nil}))
}

func TestEffectiveMatchConfig(t *testing.T) {
	t.Parallel()
	require.Nil(t, ra.EffectiveMatchConfig(nil, ""))
	require.Nil(t, ra.EffectiveMatchConfig(nil, "   "))
	require.Nil(t, ra.EffectiveMatchConfig([]byte("null"), ""))

	// A legacy regex translates into a single content/regex condition.
	raw := ra.EffectiveMatchConfig(nil, "a+")
	require.NotEmpty(t, raw)
	require.NoError(t, ra.ValidateMatchConfig(raw))

	// A stored config takes precedence over the legacy regex.
	stored := []byte(`{"conditions":[{"target":"content","op":"equals","value":"x"}]}`)
	require.Equal(t, stored, ra.EffectiveMatchConfig(stored, "ignored"))
}

func TestValidateMatchConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
	}{
		{"nil", "", false},
		{"null", "null", false},
		{"valid regex", `{"conditions":[{"target":"content","op":"regex","value":"a+"}]}`, false},
		{"valid tool_args", `{"conditions":[{"target":"tool_args","op":"equals","path":"$.x","value":"1"}]}`, false},
		{"empty conditions", `{"conditions":[]}`, true},
		{"unknown target", `{"conditions":[{"target":"bogus","op":"regex","value":"a"}]}`, true},
		{"unknown op", `{"conditions":[{"target":"content","op":"bogus","value":"a"}]}`, true},
		{"missing op", `{"conditions":[{"target":"content","value":"a"}]}`, true},
		{"bad regex", `{"conditions":[{"target":"content","op":"regex","value":"("}]}`, true},
		{"tool_args without path matches raw args", `{"conditions":[{"target":"tool_args","op":"contains","value":"rm"}]}`, false},
		{"keyword empty", `{"conditions":[{"target":"content","op":"keyword","values":[]}]}`, true},
		{"glob empty", `{"conditions":[{"target":"content","op":"glob","value":""}]}`, true},
		{"equals empty value ok", `{"conditions":[{"target":"tool_server","op":"equals","value":""}]}`, false},
	}
	for _, tc := range cases {
		err := ra.ValidateMatchConfig([]byte(tc.cfg))
		if tc.wantErr {
			require.Errorf(t, err, "case %q expected error", tc.name)
		} else {
			require.NoErrorf(t, err, "case %q expected no error", tc.name)
		}
	}
}
