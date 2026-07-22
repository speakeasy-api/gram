package efficacy

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestParseVerdictNormalizesModelOutput(t *testing.T) {
	t.Parallel()

	got, err := ParseVerdict(`
		{"score":0.75,"rationale":" the agent followed the skill ","est_turns_saved":2,
		 "est_minutes_saved":7.5,"roi_confidence":"med","flags":["partially_followed"]}
	`)

	require.NoError(t, err)
	require.InDelta(t, 0.75, got.Score, 0)
	require.Equal(t, "the agent followed the skill", got.Rationale)
	require.NotNil(t, got.EstTurnsSaved)
	require.InDelta(t, 2.0, *got.EstTurnsSaved, 0)
	require.NotNil(t, got.EstMinutesSaved)
	require.InDelta(t, 7.5, *got.EstMinutesSaved, 0)
	require.Equal(t, new("med"), got.ROIConfidence)
	require.Equal(t, []string{"partially_followed"}, got.Flags)
}

func TestParseVerdictRejectsUnparseableOutput(t *testing.T) {
	t.Parallel()

	_, err := ParseVerdict("not json")

	require.ErrorIs(t, err, ErrModelFailure)
}

func TestNormalizeVerdictRejectsNonFiniteScore(t *testing.T) {
	t.Parallel()

	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := Verdict{Score: score}.Normalize()
		require.ErrorIs(t, err, ErrModelFailure)
	}
}

func TestNormalizeVerdictClampsScore(t *testing.T) {
	t.Parallel()

	high, err := Verdict{Score: 4.2}.Normalize()
	require.NoError(t, err)
	require.InDelta(t, 1.0, high.Score, 0)

	low, err := Verdict{Score: -3}.Normalize()
	require.NoError(t, err)
	require.InDelta(t, 0.0, low.Score, 0)
}

func TestNormalizeVerdictTruncatesRationaleByRune(t *testing.T) {
	t.Parallel()

	got, err := Verdict{Score: 0.5, Rationale: strings.Repeat("é", maxRationaleRunes+50)}.Normalize()

	require.NoError(t, err)
	require.Equal(t, maxRationaleRunes, utf8.RuneCountInString(got.Rationale))
	require.True(t, utf8.ValidString(got.Rationale))
}

func TestNormalizeVerdictNullsOutOfDomainROI(t *testing.T) {
	t.Parallel()

	got, err := Verdict{
		Score:           0.5,
		EstTurnsSaved:   new(-1.0),
		EstMinutesSaved: new(math.Inf(1)),
		ROIConfidence:   new("extremely-high"),
	}.Normalize()

	require.NoError(t, err)
	require.Nil(t, got.EstTurnsSaved)
	require.Nil(t, got.EstMinutesSaved)
	require.Nil(t, got.ROIConfidence)
}

func TestNormalizeVerdictKeepsZeroROIEstimate(t *testing.T) {
	t.Parallel()

	got, err := Verdict{Score: 0.5, EstTurnsSaved: new(0.0)}.Normalize()

	require.NoError(t, err)
	require.NotNil(t, got.EstTurnsSaved)
	require.InDelta(t, 0.0, *got.EstTurnsSaved, 0)
}

func TestNormalizeVerdictDropsUnknownAndDuplicateFlags(t *testing.T) {
	t.Parallel()

	got, err := Verdict{
		Score: 0.5,
		Flags: []string{"harmful", "invented", "harmful", "ignored"},
	}.Normalize()

	require.NoError(t, err)
	require.Equal(t, []string{"harmful", "ignored"}, got.Flags)
}
