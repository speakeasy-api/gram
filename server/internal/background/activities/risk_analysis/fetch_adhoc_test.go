package risk_analysis_test

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/uuidv7"
)

// futureUpperBound sorts after all UUIDv7 values generated during the test.
func futureUpperBound() uuid.UUID {
	return uuidv7.LowerBound(time.Now().Add(time.Hour))
}

func TestFetchAdhoc_ReturnsMessagesInWindow(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgs := seedMessages(t, conn, td, 3)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 3)
	require.Len(t, result.Policies, 1)
	require.Equal(t, td.policyID, result.Policies[0].ID)
	for _, id := range msgs {
		require.True(t, slices.Contains(result.MessageIDs, id))
	}
}

func TestFetchAdhoc_IncludesAlreadyAnalyzedMessages(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	msgs := seedMessages(t, conn, td, 2)

	// The live coordinator would skip these; ad-hoc runs must not.
	err := riskrepo.New(conn).MarkMessagesRiskAnalyzed(t.Context(), riskrepo.MarkMessagesRiskAnalyzedParams{
		ProjectID:  uuid.NullUUID{UUID: td.projectID, Valid: true},
		MessageIds: msgs,
	})
	require.NoError(t, err)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.MessageIDs, 2)
}

func TestFetchAdhoc_UpperBoundExcludesLaterMessages(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 2)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: uuidv7.LowerBound(time.Now().Add(-time.Hour)),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Empty(t, result.MessageIDs)
	require.Len(t, result.Policies, 1)
}

func TestFetchAdhoc_CursorPaginatesAscending(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 5)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)

	var all []uuid.UUID
	cursor := uuid.Nil
	for range 3 {
		result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
			ProjectID:    td.projectID,
			RiskPolicyID: uuid.NullUUID{},
			IDLowerBound: zeroLowerBound,
			IDUpperBound: futureUpperBound(),
			IDCursor:     cursor,
			BatchLimit:   2,
		})
		require.NoError(t, err)
		require.True(t, slices.IsSorted(conv(result.MessageIDs)), "pages must come back in ascending id order")
		all = append(all, result.MessageIDs...)
		if len(result.MessageIDs) > 0 {
			cursor = result.MessageIDs[len(result.MessageIDs)-1]
		}
	}
	require.Len(t, all, 5)
	require.Len(t, uniq(all), 5, "keyset pagination must not repeat messages across pages")
}

func TestFetchAdhoc_SpecificPolicyScopesRun(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 1)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{UUID: td.policyID, Valid: true},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Len(t, result.Policies, 1)
	require.Equal(t, td.policyID, result.Policies[0].ID)
}

func TestFetchAdhoc_UnknownPolicyErrors(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 1)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	_, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.Error(t, err, "a requested policy that is not enabled must fail the run")
}

func TestFetchAdhoc_NoEnabledPoliciesReturnsEmpty(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, false)
	seedMessages(t, conn, td, 1)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	result, err := activity.Fetch(t.Context(), risk_analysis.FetchAdhocArgs{
		ProjectID:    td.projectID,
		RiskPolicyID: uuid.NullUUID{},
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
		IDCursor:     uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Empty(t, result.Policies)
	require.Empty(t, result.MessageIDs)
}

func TestCountAdhoc_CountsWindow(t *testing.T) {
	t.Parallel()
	conn := cloneDB(t)
	td := seedTestData(t, conn, true)
	seedMessages(t, conn, td, 4)

	activity := risk_analysis.NewFetchAdhoc(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn)
	total, err := activity.Count(t.Context(), risk_analysis.CountAdhocArgs{
		ProjectID:    td.projectID,
		IDLowerBound: zeroLowerBound,
		IDUpperBound: futureUpperBound(),
	})
	require.NoError(t, err)
	require.Equal(t, int64(4), total)

	total, err = activity.Count(t.Context(), risk_analysis.CountAdhocArgs{
		ProjectID:    td.projectID,
		IDLowerBound: zeroLowerBound,
		IDUpperBound: uuidv7.LowerBound(time.Now().Add(-time.Hour)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
}

func conv(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func uniq(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	var out []uuid.UUID
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
