package risk_analysis_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestAnalyzeBatch_EmptyMessageIDs(t *testing.T) {
	t.Parallel()
	ab := risk_analysis.NewAnalyzeBatch(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, &risk_analysis.StubPIIScanner{})
	require.NotNil(t, ab)

	result, err := ab.Do(t.Context(), risk_analysis.AnalyzeBatchArgs{
		MessageIDs: nil,
		Sources:    []string{"gitleaks"},
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

	// PresidioClient pointed at a dead URL simulates Presidio being down
	deadClient := risk_analysis.NewPresidioClient(
		"http://127.0.0.1:1",
		&http.Client{Timeout: 1 * time.Second},
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		testenv.NewLogger(t),
	)

	ab := risk_analysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		deadClient,
	)

	// Execute via Temporal test activity environment to satisfy activity.RecordHeartbeat
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.RegisterActivity(ab.Do)

	val, err := env.ExecuteActivity(ab.Do, risk_analysis.AnalyzeBatchArgs{
		ProjectID:      td.projectID,
		OrganizationID: td.orgID,
		RiskPolicyID:   td.policyID,
		PolicyVersion:  td.policyVersion,
		MessageIDs:     []uuid.UUID{msgID},
		Sources:        []string{"gitleaks", "presidio"},
	})
	require.NoError(t, err, "should not fail when presidio is down")

	var result risk_analysis.AnalyzeBatchResult
	require.NoError(t, val.Get(&result))
	assert.Equal(t, 1, result.Processed)
	assert.Positive(t, result.Findings, "gitleaks findings should be preserved when presidio is down")
}
