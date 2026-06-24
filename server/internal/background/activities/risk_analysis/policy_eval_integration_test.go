package risk_analysis_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// TestPolicyEvalRun_IsolatedFromEnforcement is the core isolation guarantee:
// an eval run scans (even a disabled policy), writes findings to
// policy_eval_findings, and rolls up stats — but never touches risk_results,
// chat_messages.risk_analyzed_at, or the outbox.
func TestPolicyEvalRun_IsolatedFromEnforcement(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	// Seed a DISABLED policy: an eval must still scan it — trying a not-yet-enabled
	// policy is the entire point of session replay.
	td := seedTestData(t, conn, false)

	msgID, err := testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{
		ChatID:    td.chatID,
		ProjectID: uuid.NullUUID{UUID: td.projectID, Valid: true},
		Role:      "user",
		Content:   "AWS key AKIAIOSFODNN7REALKEY in here",
	})
	require.NoError(t, err)

	// Pending eval run over a manual sample pinned to our one message.
	runID, err := uuid.NewV7()
	require.NoError(t, err)
	spec := risk_analysis.EvalSampleSpec{Mode: "manual", MaxMessages: 10, MessageIDs: []string{msgID.String()}}
	sampleJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	_, err = riskrepo.New(conn).CreatePolicyEvalRun(t.Context(), riskrepo.CreatePolicyEvalRunParams{
		ID:                runID,
		ProjectID:         td.projectID,
		OrganizationID:    td.orgID,
		RiskPolicyID:      uuid.NullUUID{UUID: td.policyID, Valid: true},
		RiskPolicyVersion: pgtype.Int8{Int64: td.policyVersion, Valid: true},
		ConfigSnapshot:    nil,
		SampleDefinition:  sampleJSON,
		RequestedBy:       pgtype.Text{},
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	scanner := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t),
		conn, &risk_analysis.StubPIIScanner{}, nil, nil, nil, nil, nil,
		newPresidioPub(), newGitleaksPub(), mustCELEngine(t),
	)
	pe := risk_analysis.NewPolicyEval(testenv.NewLogger(t), conn, scanner)
	ref := risk_analysis.PolicyEvalRunRef{RunID: runID, ProjectID: td.projectID}

	// Run the activities through the Temporal test env (RunScan heartbeats).
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(pe.SelectSample)
	env.RegisterActivity(pe.RunScan)
	env.RegisterActivity(pe.CompleteRun)

	_, err = env.ExecuteActivity(pe.SelectSample, ref)
	require.NoError(t, err)

	scanVal, err := env.ExecuteActivity(pe.RunScan, ref)
	require.NoError(t, err)
	var scanRes risk_analysis.PolicyEvalScanResult
	require.NoError(t, scanVal.Get(&scanRes))
	require.Equal(t, 1, scanRes.MessagesScanned)
	require.Positive(t, scanRes.FindingsCount, "gitleaks should flag the AWS key even though the policy is disabled")

	// Findings landed in policy_eval_findings...
	evalFindings, err := riskrepo.New(conn).ListPolicyEvalFindings(t.Context(), riskrepo.ListPolicyEvalFindingsParams{
		ProjectID:       td.projectID,
		PolicyEvalRunID: runID,
		CursorCreatedAt: pgtype.Timestamptz{},
		CursorID:        uuid.NullUUID{},
		ResultLimit:     100,
	})
	require.NoError(t, err)
	require.NotEmpty(t, evalFindings, "gitleaks findings should land in policy_eval_findings")

	// ...and the enforcement table is untouched (no risk_results means writeResults
	// never ran, which also means no RiskFindingCreated outbox events were appended,
	// since the outbox write is inside the same writeResults transaction).
	rrRows, err := testrepo.New(conn).ListRiskResultsAll(t.Context(), testrepo.ListRiskResultsAllParams{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
	})
	require.NoError(t, err)
	require.Empty(t, rrRows, "eval must not write enforcement results")

	// The scanned message must not be marked analyzed: it is still returned by the
	// unanalyzed-message query (which filters risk_analyzed_at IS NULL).
	unanalyzed, err := riskrepo.New(conn).FetchUnanalyzedMessageIDs(t.Context(), riskrepo.FetchUnanalyzedMessageIDsParams{
		ProjectID:    uuid.NullUUID{UUID: td.projectID, Valid: true},
		IDLowerBound: uuid.Nil,
		BatchLimit:   1000,
	})
	require.NoError(t, err)
	require.Contains(t, unanalyzed, msgID, "eval must not set risk_analyzed_at")

	// CompleteRun rolls up the stats onto the run header.
	_, err = env.ExecuteActivity(pe.CompleteRun, ref, scanRes)
	require.NoError(t, err)
	run, err := riskrepo.New(conn).GetPolicyEvalRun(t.Context(), riskrepo.GetPolicyEvalRunParams{ID: runID, ProjectID: td.projectID})
	require.NoError(t, err)
	require.Equal(t, "completed", run.Status)
	require.Positive(t, run.FindingsCount)
	require.True(t, run.CompletedAt.Valid)
}
