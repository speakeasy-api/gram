package risk_analysis_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestFetchUnanalyzed_ReturnsUnscannedMessages(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 3)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 3)
	require.Equal(t, td.orgID, result.OrganizationID)
	require.Equal(t, td.policyVersion, result.PolicyVersion)
}

func TestFetchUnanalyzed_ExcludesAlreadyAnalyzed(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgs := seedMessages(t, conn, td, 2)

	// Mark first message as analyzed
	resultID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskrepo.New(conn).InsertRiskResults(t.Context(), []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         td.projectID,
		OrganizationID:    td.orgID,
		RiskPolicyID:      td.policyID,
		RiskPolicyVersion: td.policyVersion,
		ChatMessageID:     msgs[0],
		Source:            "gitleaks",
		Found:             false,
		RuleID:            pgtype.Text{},
		Description:       pgtype.Text{},
		Match:             pgtype.Text{},
		StartPos:          pgtype.Int4{},
		EndPos:            pgtype.Int4{},
		Confidence:        pgtype.Float8{},
		Tags:              nil,
	}})
	require.NoError(t, err)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 1)
	require.Equal(t, msgs[1], result.MessageIDs[0])
}

func TestFetchUnanalyzed_DisabledPolicyReturnsEmpty(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, false)
	seedMessages(t, conn, td, 1)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Empty(t, result.MessageIDs)
}

func TestFetchUnanalyzed_DeletedPolicyReturnsEmpty(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 1)

	err := riskrepo.New(conn).DeleteRiskPolicy(t.Context(), riskrepo.DeleteRiskPolicyParams{
		ID:        td.policyID,
		ProjectID: td.projectID,
	})
	require.NoError(t, err)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Empty(t, result.MessageIDs)
}

func TestFetchUnanalyzed_RespectsBatchLimit(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 5)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: td.policyID,
		BatchLimit:   2,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 2)
}
