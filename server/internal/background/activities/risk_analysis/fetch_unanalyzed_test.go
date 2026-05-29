package risk_analysis_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// zeroLowerBound is a UUID that sorts before all real UUIDv7 values, so it
// effectively means "no lower-bound filter" in tests.
var zeroLowerBound = uuid.UUID{}

func TestFetchUnanalyzed_ReturnsUnscannedMessages(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 3)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		IDLowerBound: zeroLowerBound,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 3)
	require.Len(t, result.Policies, 1)
	require.Equal(t, td.policyID, result.Policies[0].ID)
	require.Equal(t, td.orgID, result.Policies[0].OrganizationID)
	require.Equal(t, td.policyVersion, result.Policies[0].Version)
}

func TestFetchUnanalyzed_ExcludesAlreadyAnalyzed(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgs := seedMessages(t, conn, td, 2)

	// Mark first message as analyzed via risk_analyzed_at.
	err := riskrepo.New(conn).MarkMessagesRiskAnalyzed(t.Context(), riskrepo.MarkMessagesRiskAnalyzedParams{
		ProjectID:  uuid.NullUUID{UUID: td.projectID, Valid: true},
		MessageIds: []uuid.UUID{msgs[0]},
	})
	require.NoError(t, err)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		IDLowerBound: zeroLowerBound,
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
		IDLowerBound: zeroLowerBound,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Empty(t, result.MessageIDs)
	require.Empty(t, result.Policies)
}

func TestFetchUnanalyzed_RespectsBatchLimit(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 5)

	activity := risk_analysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Do(t.Context(), risk_analysis.FetchUnanalyzedArgs{
		ProjectID:    td.projectID,
		IDLowerBound: zeroLowerBound,
		BatchLimit:   2,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 2)
}
