package customruleanalyzer_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
)

// scanReq builds a content-only scan request for the seeded project.
func scanReq(p seededProject, content string, ruleIDs ...string) customruleanalyzer.ScanRequest {
	return customruleanalyzer.ScanRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: ruleIDs,
		Content:       content,
		Kind:          "user_message",
		ToolCalls:     nil,
	}
}

func TestScan_MatchReturnsFinding(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "here is a secret value", "custom.secret")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	f := findings[0]
	require.Equal(t, "custom.secret", f.RuleID)
	require.Equal(t, "test rule description", f.Description)
	require.Equal(t, "custom", f.Source)
	require.InDelta(t, 1.0, f.Confidence, 0.0001)
	require.Equal(t, "secret", f.Match)
	// Byte offsets must slice the match out of the content.
	require.Equal(t, req.Content[f.StartPos:f.EndPos], f.Match)
	// Tags is an initialized empty slice, never nil.
	require.NotNil(t, f.Tags)
	require.Empty(t, f.Tags)
	// Span attribution: a content match is keyed to the "content" field with no
	// tool-call group key and no JSON sub-path.
	require.Equal(t, "content", f.Field)
	require.Empty(t, f.SpanGroupKey)
	require.Empty(t, f.Path)
}

// A rule whose predicate matches more than once records one span per occurrence,
// and Scan emits a Finding per span.
func TestScan_MultipleSpansYieldFindingPerMatch(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "secret then another secret", "custom.secret")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, findings, 2)
	for _, f := range findings {
		require.Equal(t, "secret", f.Match)
		require.Equal(t, req.Content[f.StartPos:f.EndPos], f.Match)
	}
}

// Only the rule that actually matches contributes findings even when several
// rules are selected.
func TestScan_MultipleRulesOnlyMatchingFire(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)
	seedCustomRule(t, conn, p, "custom.token", `content.matchRegex("token")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "here is a secret value", "custom.secret", "custom.token")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, "custom.secret", findings[0].RuleID)
}

// A rule targeting tool_calls resolves against the tools rebuilt from the
// request's tool calls, with Server/Function derived from each tool name.
func TestScan_ToolCallRuleMatches(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.deltool", `tool_calls.exists(t, t.function.matchRegex("delete_file"))`)

	scanner := newTestScanner(t, conn)
	req := customruleanalyzer.ScanRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: []string{"custom.deltool"},
		Content:       "",
		Kind:          "tool_request",
		ToolCalls:     []customruleanalyzer.ScanToolCall{{Name: "mcp__fs__delete_file", Arguments: "{}"}},
	}

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	f := findings[0]
	require.Equal(t, "custom.deltool", f.RuleID)
	require.Equal(t, "delete_file", f.Match)
	// Span attribution: the match is on the tool.function field, grouped by the
	// originating tool-call name, with no JSON sub-path.
	require.Equal(t, "tool.function", f.Field)
	require.Equal(t, "mcp__fs__delete_file", f.SpanGroupKey)
	require.Empty(t, f.Path)
}

// A rule that drills into a tool call's JSON arguments records the gjson
// sub-path on the finding, alongside the tool.args field and the tool-call
// group key — exercising the Field/SpanGroupKey/Path span wiring together.
func TestScan_ToolArgsGetPopulatesPath(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.dropsql", `tool_calls.exists(t, t.args.get("command").matchRegex("DROP TABLE"))`)

	scanner := newTestScanner(t, conn)
	req := customruleanalyzer.ScanRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: []string{"custom.dropsql"},
		Content:       "",
		Kind:          "tool_request",
		ToolCalls:     []customruleanalyzer.ScanToolCall{{Name: "shell:run_bash_command", Arguments: `{"command":"DROP TABLE users"}`}},
	}

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, findings, 1)

	f := findings[0]
	require.Equal(t, "custom.dropsql", f.RuleID)
	require.Equal(t, "DROP TABLE", f.Match)
	require.Equal(t, "tool.args", f.Field)
	require.Equal(t, "shell:run_bash_command", f.SpanGroupKey)
	require.Equal(t, "command", f.Path)
}

func TestScan_CleanContentNoFindings(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "totally benign message", "custom.secret")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, findings)
	require.Empty(t, findings)
}

// Content matches the rule, but the selected id does not, so nothing is
// evaluated.
func TestScan_UnselectedRuleSkipped(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "here is a secret value", "custom.other")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, findings)
}

// With no rule ids selected LoadSelected short-circuits and Scan returns an
// initialized, empty slice rather than nil.
func TestScan_NoRulesSelectedReturnsEmpty(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "here is a secret value")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, findings)
	require.Empty(t, findings)
}

// A syntactically invalid CEL predicate surfaces as an evaluation error keyed by
// the offending rule id.
func TestScan_InvalidExpressionErrors(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.broken", `this is not valid cel !!!`)

	scanner := newTestScanner(t, conn)
	req := scanReq(p, "here is a secret value", "custom.broken")

	_, err := scanner.Scan(t.Context(), req)
	require.ErrorContains(t, err, `evaluate custom rule "custom.broken"`)
}

// ScanBatch evaluates the selected rules against every message and returns an
// index-aligned result: matching messages carry findings, clean ones an
// initialized empty slice.
func TestScanBatch_IndexAlignedFindings(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	out, err := scanner.ScanBatch(t.Context(), customruleanalyzer.ScanBatchRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: []string{"custom.secret"},
		Messages: []customruleanalyzer.ScanMessage{
			{Content: "here is a secret value", Kind: "user_message"},
			{Content: "totally benign message", Kind: "user_message"},
			{Content: "secret then another secret", Kind: "user_message"},
		},
	})
	require.NoError(t, err)
	require.Len(t, out, 3)

	require.Len(t, out[0], 1)
	require.Equal(t, "custom.secret", out[0][0].RuleID)

	require.NotNil(t, out[1])
	require.Empty(t, out[1])

	require.Len(t, out[2], 2)
}

// ScanBatch over an empty message slice returns an empty, non-nil result.
func TestScanBatch_EmptyMessages(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	out, err := scanner.ScanBatch(t.Context(), customruleanalyzer.ScanBatchRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: []string{"custom.secret"},
		Messages:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Empty(t, out)
}

// With no rule ids selected ScanBatch short-circuits after the single rule load
// and returns an initialized empty slice per message.
func TestScanBatch_NoRulesSelectedReturnsPerMessageEmpty(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	p := seedProject(t, conn)
	seedCustomRule(t, conn, p, "custom.secret", `content.matchRegex("secret")`)

	scanner := newTestScanner(t, conn)
	out, err := scanner.ScanBatch(t.Context(), customruleanalyzer.ScanBatchRequest{
		ProjectID:     p.projectID,
		CustomRuleIDs: nil,
		Messages: []customruleanalyzer.ScanMessage{
			{Content: "here is a secret value", Kind: "user_message"},
			{Content: "another secret", Kind: "user_message"},
		},
	})
	require.NoError(t, err)
	require.Len(t, out, 2)
	for _, findings := range out {
		require.NotNil(t, findings)
		require.Empty(t, findings)
	}
}

// A project with no matching rules at all yields no findings and no error.
func TestScan_UnknownProjectNoFindings(t *testing.T) {
	t.Parallel()

	conn := cloneDB(t)
	scanner := newTestScanner(t, conn)

	// A valid but unseeded project id: no rules exist for it.
	req := scanReq(seededProject{projectID: uuid.New()}, "here is a secret value", "custom.secret")

	findings, err := scanner.Scan(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, findings)
}
