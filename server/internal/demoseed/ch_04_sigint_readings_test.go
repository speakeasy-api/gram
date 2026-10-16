package demoseed

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
)

func validProbability(p float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= 1
}

// The seeded readings skip the Pub/Sub writer's parser and the table has no
// CHECK constraints, so this holds them to the parser's rules.
func TestSeededSigintReadingsSatisfyWriterRules(t *testing.T) {
	t.Parallel()
	s := &seeder{spec: DefaultSpec(), q: nil, logger: nil, now: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	rows, err := s.sigintReadingRows()
	require.NoError(t, err)
	type attempt struct {
		id      uuid.UUID
		attempt uuid.UUID
	}
	ids := make(map[uuid.UUID]struct{}, len(rows))
	attempts := make(map[attempt]struct{}, len(rows))
	latest := make(map[uuid.UUID]chrepo.ReadingRow, len(rows))
	for _, row := range rows {
		ids[row.ID] = struct{}{}
		attempts[attempt{row.ID, row.EvaluationAttemptID}] = struct{}{}
		if current, ok := latest[row.ID]; !ok || row.EvaluatedAt.After(current.EvaluatedAt) {
			latest[row.ID] = row
		}
		for _, id := range []uuid.UUID{row.ID, row.EvaluationAttemptID, row.ProjectID, row.SensorID} {
			require.NotEqual(t, uuid.Nil, id)
		}
		require.NotEmpty(t, row.OrganizationID)
		require.False(t, row.EventOccurredAt.After(row.EvaluatedAt))
		require.NotNil(t, row.ConversationID)
		require.NotEmpty(t, row.ActorUserEmail)
		if row.Status != chrepo.StatusAnswered {
			require.Contains(t, []string{chrepo.StatusNotApplicable, chrepo.StatusFailed}, row.Status)
			require.Equal(t, row.Status == chrepo.StatusFailed, row.FailureReason != "", "only a failure carries a reason")
			require.Empty(t, row.ResultType)
			require.Empty(t, row.Probabilities)
			require.Nil(t, row.Confidence)
			continue
		}
		require.Empty(t, row.FailureReason)
		require.NotEmpty(t, row.ResultType)
		require.NotEmpty(t, row.Probabilities)
		require.LessOrEqual(t, len(row.Attributes), chrepo.MaxAttributes)
		require.GreaterOrEqual(t, row.InputMessageCount, uint8(1))
		require.LessOrEqual(t, row.InputMessageCount, row.InputRequestedMessageCount)

		signals := make(map[uuid.UUID]string, len(row.Probabilities))
		for _, p := range row.Probabilities {
			require.True(t, validProbability(p.Probability), "probability %v out of range", p.Probability)
			require.NotEmpty(t, p.SignalSlug)
			require.NotContains(t, signals, p.SignalID, "duplicate signal")
			signals[p.SignalID] = p.SignalSlug
		}
		if row.ResultType == "multi_label" {
			require.Nil(t, row.Confidence)
		} else {
			require.NotNil(t, row.Confidence, "the classifier reports confidence for every choice and score")
			require.True(t, validProbability(*row.Confidence))
		}
		switch row.ResultType {
		case "choice":
			require.NotNil(t, row.SelectedSignalID)
			require.Contains(t, signals, *row.SelectedSignalID, "selected signal missing from distribution")
			require.Equal(t, signals[*row.SelectedSignalID], row.SelectedSignalSlug)
		case "score":
			require.NotNil(t, row.ExpectedIndex)
			require.GreaterOrEqual(t, *row.ExpectedIndex, 0.0)
			require.LessOrEqual(t, *row.ExpectedIndex, float64(len(row.Probabilities)-1))
		case "multi_label":
			require.Nil(t, row.SelectedSignalID)
			require.Nil(t, row.ExpectedIndex)
		default:
			require.Failf(t, "unknown result type", "result type %q", row.ResultType)
		}
	}
	require.Len(t, ids, sigintReadingDays*3, "one reading per message and sensor")
	require.Len(t, attempts, len(rows), "a retry is a new attempt of the same reading")
	statuses := map[string]int{}
	for _, row := range latest {
		statuses[row.Status]++
	}
	require.Positive(t, statuses[chrepo.StatusNotApplicable])
	require.Positive(t, statuses[chrepo.StatusFailed])
	require.Greater(t, len(rows), len(ids), "some readings keep a superseded failed attempt")
	for _, row := range rows {
		if row.Status == chrepo.StatusFailed && latest[row.ID].Status == chrepo.StatusAnswered {
			return
		}
	}
	require.Fail(t, "no failed attempt was retried into an answer")
}
