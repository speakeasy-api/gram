package demoseed

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sigint/chrepo"
	"github.com/speakeasy-api/gram/server/internal/sigint/readings"
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
	require.Len(t, rows, (sigintReadingDays+sigintManualRuns*sigintManualMessages)*3)

	ids := make(map[uuid.UUID]struct{}, len(rows))
	manualRuns := make(map[uuid.UUID]int)
	streaming := 0
	attributeValues := make(map[string]map[string]bool)
	for _, row := range rows {
		ids[row.ID] = struct{}{}
		for _, id := range []uuid.UUID{row.ID, row.EvaluationAttemptID, row.ProjectID, row.SensorID} {
			require.NotEqual(t, uuid.Nil, id)
		}
		require.NotEmpty(t, row.OrganizationID)
		require.Equal(t, chrepo.StatusAnswered, row.Status)
		require.Empty(t, row.FailureReason)
		require.NotEmpty(t, row.ResultType)
		require.NotEmpty(t, row.Probabilities)
		require.LessOrEqual(t, len(row.Attributes), chrepo.MaxAttributes)
		for _, key := range []string{"model_name", "model_family", "model_provider", "department"} {
			value, present := row.Attributes[key]
			if present {
				require.NotEmpty(t, value)
				require.NotEqual(t, "<unset>", value)
			}
			if attributeValues[key] == nil {
				attributeValues[key] = make(map[string]bool)
			}
			attributeValues[key][value] = true
		}
		require.GreaterOrEqual(t, row.InputMessageCount, uint8(1))
		require.LessOrEqual(t, row.InputMessageCount, row.InputRequestedMessageCount)
		require.False(t, row.EventOccurredAt.After(row.EvaluatedAt))
		request := readings.Context{TriggerKind: readings.TriggerKind(row.TriggerKind), RunID: row.RunID, RequestedAt: row.EvaluationRequestedAt}
		require.NoError(t, request.Validate())
		require.Equal(t, request.ReadingID(row.OrganizationID, row.ProjectID.String(), row.EventKind, row.EventID, row.SensorID.String()), row.ID)
		require.False(t, row.EvaluationRequestedAt.After(row.EvaluatedAt))
		if row.TriggerKind == string(readings.TriggerManual) {
			manualRuns[row.RunID]++
		} else {
			streaming++
		}

		signals := make(map[uuid.UUID]string, len(row.Probabilities))
		for _, p := range row.Probabilities {
			require.True(t, validProbability(p.Probability), "probability %v out of range", p.Probability)
			require.NotEmpty(t, p.SignalSlug)
			require.NotContains(t, signals, p.SignalID, "duplicate signal")
			signals[p.SignalID] = p.SignalSlug
		}
		if row.Confidence != nil {
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
	require.Len(t, ids, len(rows), "reading ids must be unique")
	for key, values := range attributeValues {
		require.Contains(t, values, "", "%s must demonstrate unknown values", key)
		require.GreaterOrEqual(t, len(values), 3, "%s must demonstrate breakdowns", key)
	}
	require.Equal(t, sigintReadingDays*3, streaming)
	require.Len(t, manualRuns, sigintManualRuns)
	for _, count := range manualRuns {
		require.Equal(t, sigintManualMessages*3, count)
	}
}
