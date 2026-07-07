package risk_analysis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	tsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type recordingPromptJudge struct {
	inputs []risk_analysis.JudgeInput
}

func (j *recordingPromptJudge) Evaluate(_ context.Context, in risk_analysis.JudgeInput) *risk_analysis.JudgeVerdict {
	j.inputs = append(j.inputs, in)
	return &risk_analysis.JudgeVerdict{
		Matched:          true,
		Confidence:       0.9,
		Rationale:        "matched tool call",
		CostUSD:          0,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}
}

// newPresidioPub returns a mock presidio publisher that accepts any publish
// call and reports success. Tests that don't enable the "presidio" source
// never invoke it; tests that do get a fire-and-forget success. Wiring a real
// mock (never nil) keeps the publish path off the nil-deref cliff.
func newPresidioPub() *gcp.MockPublisher[*riskv1.PresidioAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.PresidioAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

// mustCELEngine builds a real CEL engine for tests; construction is
// deterministic so a failure is a fatal setup error.
func mustCELEngine(t *testing.T) *celenv.Engine {
	t.Helper()
	eng, err := celenv.New()
	require.NoError(t, err)
	return eng
}

func newGitleaksPub() *gcp.MockPublisher[*riskv1.GitleaksAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.GitleaksAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func newCustomRulesPub() *gcp.MockPublisher[*riskv1.CustomRulesAnalysis] {
	pub := gcp.NewMockPublisher[*riskv1.CustomRulesAnalysis]()
	pub.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult())
	return pub
}

func TestAnalyzeBatch_EmptyMessageIDs(t *testing.T) {
	t.Parallel()
	ab := risk_analysis.NewAnalyzeBatch(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, &risk_analysis.StubPIIScanner{}, nil, nil, nil, nil, nil, newPresidioPub(), newGitleaksPub(), newCustomRulesPub(), mustCELEngine(t), nil)
	require.NotNil(t, ab)

	result, err := ab.Do(t.Context(), risk_analysis.AnalyzeBatchArgs{
		ProjectID:        uuid.Nil,
		OrganizationID:   "",
		RiskPolicyID:     uuid.Nil,
		PolicyVersion:    0,
		MessageIDs:       nil,
		Sources:          []string{"gitleaks"},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)
}

func TestAnalyzeBatch_GracefulDegradationWhenPresidioDown(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	// Insert a message with a gitleaks-detectable secret
	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY and email alice@example.com",
	})
	require.NoError(t, err)

	// Point the production PresidioClient at a dead URL so the activity
	// path mirrors what runs in the worker. After exhausting the retry
	// budget the message dead-letters and the activity proceeds — gitleaks
	// findings on the same message survive.
	piiScanner := risk_analysis.NewPresidioClient(
		"http://127.0.0.1:1",
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		testenv.NewLogger(t),
	)

	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		piiScanner,
		nil,
		nil,
		nil,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
		nil,
	)

	// Execute via Temporal test activity environment to satisfy activity.RecordHeartbeat
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          []string{"gitleaks", "presidio"},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err, "should not fail when presidio is down")

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	assert.Equal(t, 1, result.Processed)
	assert.Positive(t, result.Findings, "gitleaks findings should be preserved when presidio is down")

	// Presidio dead-letter should land as its own row (found=false,
	// dead_letter_reason populated) so the failure is auditable in the DB.
	// The production list queries filter on found IS TRUE, so we read every
	// row via the test fixture query.
	rows, err := testrepo.New(conn).ListRiskResultsAll(t.Context(), testrepo.ListRiskResultsAllParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
	})
	require.NoError(t, err)

	var sawDeadLetter bool
	for _, row := range rows {
		if row.Source == "presidio" && row.DeadLetterReason.Valid && row.DeadLetterReason.String != "" {
			assert.False(t, row.Found, "dead-letter row must not be flagged as a finding")
			sawDeadLetter = true
		}
	}
	assert.True(t, sawDeadLetter, "expected a presidio dead-letter row with dead_letter_reason set")
}

func TestAnalyzeBatch_FilteredMessagesStillClearExistingResults(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "hello",
	})
	require.NoError(t, err)

	_, err = riskrepo.New(conn).InsertRiskResults(t.Context(), []riskrepo.InsertRiskResultsParams{{
		ID:                uuid.New(),
		ProjectID:         td.projectID,
		OrganizationID:    td.orgID,
		RiskPolicyID:      td.policyID,
		RiskPolicyVersion: td.policyVersion,
		ChatMessageID:     msgID,
		Source:            "gitleaks",
		Found:             true,
		RuleID:            pgtype.Text{String: "secret.test", Valid: true},
		Description:       pgtype.Text{String: "stale finding", Valid: true},
		Match:             pgtype.Text{String: "match", Valid: true},
		StartPos:          pgtype.Int4{Int32: 0, Valid: true},
		EndPos:            pgtype.Int4{Int32: 5, Valid: true},
		Confidence:        pgtype.Float8{Float64: 1, Valid: true},
		Tags:              []string{},
		DeadLetterReason:  pgtype.Text{},
	}})
	require.NoError(t, err)

	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		nil,
		nil,
		nil,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
		nil,
	)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          []string{"gitleaks"},
		MessageTypes:     []string{message.ToolRequest},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Equal(t, 0, result.Processed)
	require.Equal(t, 0, result.Findings)

	rows, err := testrepo.New(conn).ListRiskResultsAll(t.Context(), testrepo.ListRiskResultsAllParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestAnalyzeBatch_DestructiveToolAnnotationFinding(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := true
	toolsetID := seedHTTPToolset(t, conn, td, "delete_records", &destructive)
	msgID := insertAssistantToolCall(t, conn, td, "mcp__gram__delete_records", toolsetID)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, msgID, rows[0].ChatMessageID)
	require.True(t, rows[0].Found)
	require.Equal(t, shadowmcp.SourceDestructiveTool, rows[0].Source)
	require.Equal(t, "destructive.tool", rows[0].RuleID.String)
	require.Equal(t, "delete_records", rows[0].Match.String)
}

func TestAnalyzeBatch_PromptJudgeUsesToolCallPayload(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "prompt policy",
		PolicyType:     "prompt_based",
		Sources:        []string{},
		MessageTypes:   []string{message.ToolRequest},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: "Block destructive shell commands", Valid: true},
	})
	require.NoError(t, err)
	td.policyID = policy.ID
	td.policyVersion = policy.Version

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf /tmp/data"})
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, td.orgID, true)
	judge := &recordingPromptJudge{}
	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		nil,
		nil,
		nil,
		judge,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
		nil,
	)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          nil,
		MessageTypes:     []string{message.ToolRequest},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)
	require.Len(t, judge.inputs, 1)
	msg := judge.inputs[0].Message
	require.Equal(t, message.ToolRequest, msg.Type)
	require.Equal(t, "Bash", msg.ToolName)
	require.Empty(t, msg.MCPServer, "native tool has no MCP server")
	require.Empty(t, msg.MCPFunction)
	require.Contains(t, msg.Body, "rm -rf /tmp/data")
}

func TestAnalyzeBatch_PromptJudgeMultiToolCallAttribution(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		Name:           "prompt policy",
		PolicyType:     "prompt_based",
		Sources:        []string{},
		MessageTypes:   []string{message.ToolRequest},
		Enabled:        true,
		Action:         "flag",
		AudienceType:   "everyone",
		AutoName:       false,
		Prompt:         pgtype.Text{String: "Block destructive operations", Valid: true},
	})
	require.NoError(t, err)
	td.policyID = policy.ID
	td.policyVersion = policy.Version

	// An assistant message that issued two tool calls — one MCP, one native.
	msgID := insertAssistantToolCallsWithArgs(t, conn, td, []struct {
		name string
		args map[string]any
	}{
		{name: "mcp__github__delete_repo", args: map[string]any{"repo": "prod"}},
		{name: "Bash", args: map[string]any{"command": "rm -rf /tmp/data"}},
	})

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPromptPolicies, td.orgID, true)
	judge := &recordingPromptJudge{}
	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		nil,
		nil,
		nil,
		judge,
		flags,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
		nil,
	)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       []uuid.UUID{msgID},
		Sources:          nil,
		MessageTypes:     []string{message.ToolRequest},
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	require.Len(t, judge.inputs, 1)

	// The judge sees both calls, each with its own attribution — not an opaque blob.
	msg := judge.inputs[0].Message
	require.Equal(t, message.ToolRequest, msg.Type)
	require.Empty(t, msg.ToolName, "multi-call message carries no single tool name")
	require.Len(t, msg.ToolCalls, 2)

	require.Equal(t, "github", msg.ToolCalls[0].MCPServer)
	require.Equal(t, "delete_repo", msg.ToolCalls[0].MCPFunction)
	require.Contains(t, msg.ToolCalls[0].Arguments, "prod")

	require.Equal(t, "Bash", msg.ToolCalls[1].ToolName)
	require.Empty(t, msg.ToolCalls[1].MCPServer)
	require.Contains(t, msg.ToolCalls[1].Arguments, "rm -rf /tmp/data")
}

func TestAnalyzeBatch_DestructiveToolAnnotationSkipsFalseHint(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := false
	toolsetID := seedHTTPToolset(t, conn, td, "read_records", &destructive)
	msgID := insertAssistantToolCall(t, conn, td, "MCP:read_records", toolsetID)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 0, result.Findings)
}

// TestAnalyzeBatch_CLIDestructive_BashRmRf seeds a native Bash tool call
// with `rm -rf *` and asserts the cli_destructive scanner emits a finding
// keyed by the matched pattern. Native tools were previously skipped by the
// MCP-only filter in scanDestructiveToolAnnotations — proving they are now
// in scope is the core of this scenario.
func TestAnalyzeBatch_CLIDestructive_BashRmRf(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "rm -rf *"})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Found)
	assert.Equal(t, risk_analysis.SourceCLIDestructive, rows[0].Source)
	assert.Equal(t, "destructive.shell.rm_rf", rows[0].RuleID.String)
	assert.Equal(t, "Bash", rows[0].Match.String)
}

func TestAnalyzeBatch_CLIDestructive_GitForcePush(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "run_terminal_cmd", map[string]any{"command": "git push --force origin main"})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "destructive.git.push_force", rows[0].RuleID.String)
}

// TestAnalyzeBatch_CLIDestructive_MCPArgsDropTable proves the cli_destructive
// scanner is genuinely tool-name-agnostic: an MCP-routed tool whose
// `arguments` carry a destructive SQL fragment also flags. This is the
// "scan all tool calls" semantics the planner asked for.
func TestAnalyzeBatch_CLIDestructive_MCPArgsDropTable(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "mcp__db__run_query", map[string]any{"query": "DROP TABLE users"})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "destructive.database.drop", rows[0].RuleID.String)
}

// TestAnalyzeBatch_CLIDestructive_StableRuleIDAcrossKeys exercises the
// deterministic-iteration guarantee of flattenCLIStrings: a tool call whose
// arguments carry destructive content under multiple keys must report the
// same rule_id every run. Without sorted map iteration the rule_id flaps
// between matches because the first-match-wins inner loop sees keys in a
// random order.
func TestAnalyzeBatch_CLIDestructive_StableRuleIDAcrossKeys(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{
		"command": "rm -rf *",                     // shell/rm_rf
		"context": "DROP TABLE x",                 // database/drop
		"alt":     "git push --force origin main", // git/push_force
	})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	// Sorted-key iteration walks "alt" → "command" → "context", so the
	// first match is git/push_force from the "alt" key. Locking this in
	// a test catches accidental reintroduction of random map ordering.
	assert.Equal(t, "destructive.git.push_force", rows[0].RuleID.String)
}

// TestAnalyzeBatch_BothSources_OnSameMCPCall asserts that destructive_tool
// (annotation) and cli_destructive (content) emit two distinct findings on
// a single MCP tool call when both sources are enabled. Proves the dedup
// pass at the merge boundary lets non-overlapping findings through and
// neither rule_id collides.
func TestAnalyzeBatch_BothSources_OnSameMCPCall(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	destructive := true
	toolsetID := seedHTTPToolset(t, conn, td, "run_query", &destructive)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "mcp__gram__run_query", map[string]any{
		shadowmcp.XGramToolsetIDField: toolsetID.String(),
		"query":                       "DROP TABLE users",
	})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{shadowmcp.SourceDestructiveTool, risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 2, result.Findings, "destructive_tool + cli_destructive must both fire")

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ruleIDs := []string{rows[0].RuleID.String, rows[1].RuleID.String}
	assert.Contains(t, ruleIDs, "destructive.tool")
	assert.Contains(t, ruleIDs, "destructive.database.drop")
}

func TestAnalyzeBatch_CLIDestructive_BenignBash(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "Bash", map[string]any{"command": "ls -la"})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{risk_analysis.SourceCLIDestructive})
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 0, result.Findings)
}

func TestAnalyzeBatch_CustomDetectionRuleFinding(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	td = seedCustomRulePolicySelection(t, conn, td, "custom.acme_token", `content.matchRegex("ACME-[A-Z0-9]{8}")`)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "deploy with ACME-ABC12345 today",
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, nil)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Found)
	assert.Equal(t, risk_analysis.SourceCustom, rows[0].Source)
	assert.Equal(t, "custom.acme_token", rows[0].RuleID.String)
	assert.Equal(t, "ACME token", rows[0].Description.String)
	assert.Equal(t, "ACME-ABC12345", rows[0].Match.String)
}

// A configured exclusion must suppress a message-level content finding through
// the full Do() path. TestAnalyzeBatch_CustomDetectionRuleFinding is the control
// (identical setup, no exclusion -> 1 finding). The ExclusionSet predicate is
// unit-tested in isolation; the wiring the session-level work reshaped —
// policyExclusionSet's DB fetch and threading into scanStandardPolicy — is only
// exercised end-to-end here.
func TestAnalyzeBatch_ExclusionSuppressesMessageFinding(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	td = seedCustomRulePolicySelection(t, conn, td, "custom.acme_token", `content.matchRegex("ACME-[A-Z0-9]{8}")`)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "deploy with ACME-ABC12345 today",
	})
	require.NoError(t, err)

	_, err = riskrepo.New(conn).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   uuid.NullUUID{UUID: td.policyID, Valid: true},
		MatchType:      "exact",
		MatchValue:     "ACME-ABC12345",
		Enabled:        true,
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, nil)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 0, result.Findings, "excluded content finding must be suppressed end-to-end through Do()")

	// No active finding remains. The scanned message still records the empty
	// sentinel row buildRows writes, but that row is found=false, which this
	// active-findings query filters out — so the list is empty, as in
	// TestAnalyzeBatch_CustomDetectionRuleSkipsNilRegex.
	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Empty(t, rows, "no active finding should survive the exclusion")
}

func TestAnalyzeBatch_CustomDetectionRuleSkipsNilRegex(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	td = seedCustomRulePolicySelection(t, conn, td, "custom.future_rule", "")

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "future rule content",
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, nil)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 0, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// A CEL detection rule targeting the tool server flags a tool-request message
// whose MCP server matches, exercising the DB → customRuleMessageView → engine
// path.
func TestAnalyzeBatch_CustomDetectionRuleToolServer(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	td = seedCustomRulePolicySelection(t, conn, td, "custom.mise_tool", `tool_calls.exists(t, t.server.matchExact("mise"))`)

	msgID := insertAssistantToolCallWithArgs(t, conn, td, "mcp__mise__run_task", map[string]any{"task": "build"})

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, nil)
	require.Equal(t, 1, result.Processed)
	require.Equal(t, 1, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Found)
	assert.Equal(t, risk_analysis.SourceCustom, rows[0].Source)
	assert.Equal(t, "custom.mise_tool", rows[0].RuleID.String)
	assert.Equal(t, "mise", rows[0].Match.String)
}

// insertAssistantToolCallWithArgs is a sibling of insertAssistantToolCall for
// CLI scenarios where the recorded arguments don't carry a Gram toolset id —
// the cli_destructive scanner is content-driven, so the args field is the
// thing under test.
func insertAssistantToolCallWithArgs(t *testing.T, conn *pgxpool.Pool, td testData, callName string, argsMap map[string]any) uuid.UUID {
	t.Helper()

	args, err := json.Marshal(argsMap)
	require.NoError(t, err)

	toolCalls, err := json.Marshal([]map[string]any{
		{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      callName,
				"arguments": string(args),
			},
		},
	})
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), td.projectID, []chatrepo.CreateChatMessageParams{{
		ChatID:           td.chatID,
		Role:             "assistant",
		ProjectID:        td.projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{String: messageID, Valid: true},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{String: "tool_calls", Valid: true},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
	}})
	require.NoError(t, err)

	messages, err := chatrepo.New(conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    td.chatID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	for _, msg := range messages {
		if msg.MessageID.String == messageID {
			return msg.ID
		}
	}
	require.FailNow(t, "inserted tool-call message not found")
	return uuid.Nil
}

// insertAssistantToolCallsWithArgs inserts an assistant message that issued
// multiple tool calls, mirroring insertAssistantToolCallWithArgs for the
// multi-call judge path. Each entry is (tool name, arguments map).
func insertAssistantToolCallsWithArgs(t *testing.T, conn *pgxpool.Pool, td testData, calls []struct {
	name string
	args map[string]any
}) uuid.UUID {
	t.Helper()

	recorded := make([]map[string]any, 0, len(calls))
	for i, c := range calls {
		args, err := json.Marshal(c.args)
		require.NoError(t, err)
		recorded = append(recorded, map[string]any{
			"id":   fmt.Sprintf("call_%d", i+1),
			"type": "function",
			"function": map[string]any{
				"name":      c.name,
				"arguments": string(args),
			},
		})
	}
	toolCalls, err := json.Marshal(recorded)
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), td.projectID, []chatrepo.CreateChatMessageParams{{
		ChatID:           td.chatID,
		Role:             "assistant",
		ProjectID:        td.projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{String: messageID, Valid: true},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{String: "tool_calls", Valid: true},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
	}})
	require.NoError(t, err)

	messages, err := chatrepo.New(conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    td.chatID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	for _, msg := range messages {
		if msg.MessageID.String == messageID {
			return msg.ID
		}
	}
	require.FailNow(t, "inserted multi-tool-call message not found")
	return uuid.Nil
}

func executeAnalyzeBatch(t *testing.T, conn *pgxpool.Pool, td testData, messageIDs []uuid.UUID, sources []string) risk_analysis.AnalyzeBatchResult {
	t.Helper()

	accessStore := accesscontrol.NewRedisStore(cache.NoopCache, accesscontrol.AlphaTTL)
	shadowMCPClient := shadowmcp.NewClient(testenv.NewLogger(t), conn, cache.NoopCache, accessStore)
	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		&risk_analysis.StubPIIScanner{},
		nil,
		shadowMCPClient,
		nil,
		nil,
		nil,
		newPresidioPub(),
		newGitleaksPub(),
		newCustomRulesPub(),
		mustCELEngine(t),
		nil,
	)

	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:        td.projectID,
		OrganizationID:   td.orgID,
		RiskPolicyID:     td.policyID,
		PolicyVersion:    td.policyVersion,
		MessageIDs:       messageIDs,
		Sources:          sources,
		PresidioEntities: nil,
		CustomRuleIds:    nil,
	})
	require.NoError(t, err)

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	return result
}

func seedCustomRulePolicySelection(t *testing.T, conn *pgxpool.Pool, td testData, ruleID string, detectionExpr string) testData {
	t.Helper()

	detectionValue := pgtype.Text{}
	if detectionExpr != "" {
		detectionValue = pgtype.Text{String: detectionExpr, Valid: true}
	}
	_, err := riskrepo.New(conn).CreateCustomDetectionRule(t.Context(), riskrepo.CreateCustomDetectionRuleParams{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RuleID:         ruleID,
		Title:          "ACME token",
		Description:    "ACME token",
		DetectionExpr:  detectionValue,
		Severity:       "high",
	})
	require.NoError(t, err)

	policy, err := riskrepo.New(conn).GetRiskPolicy(t.Context(), riskrepo.GetRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)

	updated, err := riskrepo.New(conn).UpdateRiskPolicy(t.Context(), riskrepo.UpdateRiskPolicyParams{
		ID:                   policy.ID,
		ProjectID:            policy.ProjectID,
		Name:                 policy.Name,
		Sources:              []string{},
		PresidioEntities:     policy.PresidioEntities,
		PromptInjectionRules: policy.PromptInjectionRules,
		DisabledRules:        policy.DisabledRules,
		CustomRuleIds:        []string{ruleID},
		MessageTypes:         policy.MessageTypes,
		Enabled:              policy.Enabled,
		Action:               "flag",
		AudienceType:         policy.AudienceType,
		AutoName:             policy.AutoName,
		UserMessage:          policy.UserMessage,
	})
	require.NoError(t, err)
	td.policyVersion = updated.Version
	return td
}

func seedHTTPToolset(t *testing.T, conn *pgxpool.Pool, td testData, toolName string, destructiveHint *bool) uuid.UUID {
	t.Helper()
	ctx := t.Context()

	toolset, err := tsrepo.New(conn).CreateToolset(ctx, tsrepo.CreateToolsetParams{
		OrganizationID:         td.orgID,
		ProjectID:              td.projectID,
		Name:                   "ts-" + uuid.NewString()[:8],
		Slug:                   "ts-" + uuid.NewString()[:8],
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	deploymentID := seedCompletedDeployment(t, conn, td.projectID, td.orgID)
	toolURN := urn.NewTool(urn.ToolKindHTTP, "test-api", uuid.NewString()[:8])
	var destructive pgtype.Bool
	if destructiveHint != nil {
		destructive = pgtype.Bool{Bool: *destructiveHint, Valid: true}
	}
	_, err = deploymentsrepo.New(conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{
		ProjectID:           td.projectID,
		DeploymentID:        deploymentID,
		Openapiv3DocumentID: uuid.NullUUID{},
		ToolUrn:             toolURN,
		Name:                toolName,
		UntruncatedName:     pgtype.Text{String: "", Valid: true},
		Openapiv3Operation:  pgtype.Text{},
		Summary:             "Test tool",
		Description:         "A test tool",
		Tags:                []string{},
		Confirm:             pgtype.Text{},
		ConfirmPrompt:       pgtype.Text{},
		XGram:               pgtype.Bool{},
		OriginalName:        pgtype.Text{},
		OriginalSummary:     pgtype.Text{},
		OriginalDescription: pgtype.Text{},
		Security:            []byte("[]"),
		HttpMethod:          "POST",
		Path:                "/test",
		SchemaVersion:       "3.0.0",
		Schema:              []byte("{}"),
		HeaderSettings:      []byte("{}"),
		QuerySettings:       []byte("{}"),
		PathSettings:        []byte("{}"),
		ServerEnvVar:        "TEST_SERVER_URL",
		DefaultServerUrl:    pgtype.Text{},
		RequestContentType:  pgtype.Text{},
		ResponseFilter:      nil,
		ReadOnlyHint:        pgtype.Bool{},
		DestructiveHint:     destructive,
		IdempotentHint:      pgtype.Bool{},
		OpenWorldHint:       pgtype.Bool{},
	})
	require.NoError(t, err)

	_, err = tsrepo.New(conn).CreateToolsetVersion(ctx, tsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      []urn.Tool{toolURN},
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	return toolset.ID
}

func seedCompletedDeployment(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID, orgID string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	deployments := deploymentsrepo.New(conn)
	idempotencyKey := "test-" + uuid.NewString()

	_, err := deployments.CreateDeployment(ctx, deploymentsrepo.CreateDeploymentParams{
		IdempotencyKey: idempotencyKey,
		UserID:         "test-user",
		OrganizationID: orgID,
		ProjectID:      projectID,
		GithubRepo:     pgtype.Text{},
		GithubPr:       pgtype.Text{},
		GithubSha:      pgtype.Text{},
		ExternalID:     pgtype.Text{},
		ExternalUrl:    pgtype.Text{},
	})
	require.NoError(t, err)

	deployment, err := deployments.GetDeploymentByIdempotencyKey(ctx, deploymentsrepo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: idempotencyKey,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	for _, status := range []string{"created", "pending", "completed"} {
		_, err = deployments.TransitionDeployment(ctx, deploymentsrepo.TransitionDeploymentParams{
			DeploymentID: deployment.Deployment.ID,
			Status:       status,
			ProjectID:    projectID,
			Event:        "test",
			Message:      "test deployment status",
		})
		require.NoError(t, err)
	}

	return deployment.Deployment.ID
}

func insertAssistantToolCall(t *testing.T, conn *pgxpool.Pool, td testData, callName string, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	args, err := json.Marshal(map[string]string{
		shadowmcp.XGramToolsetIDField: toolsetID.String(),
	})
	require.NoError(t, err)

	toolCalls, err := json.Marshal([]map[string]any{
		{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      callName,
				"arguments": string(args),
			},
		},
	})
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), td.projectID, []chatrepo.CreateChatMessageParams{{
		ChatID:           td.chatID,
		Role:             "assistant",
		ProjectID:        td.projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{String: messageID, Valid: true},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{String: "tool_calls", Valid: true},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
	}})
	require.NoError(t, err)

	messages, err := chatrepo.New(conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    td.chatID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)
	for _, msg := range messages {
		if msg.MessageID.String == messageID {
			return msg.ID
		}
	}
	require.FailNow(t, "inserted tool-call message not found")
	return uuid.Nil
}

func TestAnalyzeBatch_SkipsWhenPolicyDisabled(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, false)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY",
	})
	require.NoError(t, err)

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"})
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)

	rows, err := riskrepo.New(conn).ListRiskResultsByProjectAndPolicy(t.Context(), riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		CursorID:     uuid.NullUUID{},
		PageLimit:    10,
	})
	require.NoError(t, err)
	assert.Empty(t, rows, "no risk_results should be written for a disabled policy")
}

func TestAnalyzeBatch_SkipsWhenPolicyDeleted(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY",
	})
	require.NoError(t, err)

	require.NoError(t, riskrepo.New(conn).DeleteRiskPolicy(t.Context(), riskrepo.DeleteRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	}))

	result := executeAnalyzeBatch(t, conn, td, []uuid.UUID{msgID}, []string{"gitleaks"})
	assert.Equal(t, 0, result.Processed)
	assert.Equal(t, 0, result.Findings)
}
