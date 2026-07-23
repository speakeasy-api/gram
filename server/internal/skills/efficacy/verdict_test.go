package efficacy

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
)

func TestParseSessionVerdictNormalizesModelOutput(t *testing.T) {
	t.Parallel()

	got, err := ParseSessionVerdict(`
		{"verdicts":[
			{"index":1,"score":0.25,"rationale":"barely used","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":["ignored"]},
			{"index":0,"score":0.75,"rationale":" the agent followed the skill ","est_turns_saved":2,"est_minutes_saved":7.5,"roi_confidence":"med","flags":["partially_followed"]}
		]}
	`, 2)

	require.NoError(t, err)
	require.Len(t, got, 2)
	// Verdicts come back ordered by the prompt's skill index, whatever order
	// the model emitted them in.
	require.InDelta(t, 0.75, got[0].Score, 0)
	require.Equal(t, "the agent followed the skill", got[0].Rationale)
	require.NotNil(t, got[0].EstTurnsSaved)
	require.InDelta(t, 2.0, *got[0].EstTurnsSaved, 0)
	require.Equal(t, []string{"partially_followed"}, got[0].Flags)
	require.InDelta(t, 0.25, got[1].Score, 0)
	require.Nil(t, got[1].ROIConfidence)
}

func TestParseSessionVerdictRejectsUnparseableOutput(t *testing.T) {
	t.Parallel()

	_, err := ParseSessionVerdict("not json", 1)
	require.ErrorIs(t, err, analysis.ErrModelFailure)
}

func TestParseSessionVerdictRejectsCountMismatch(t *testing.T) {
	t.Parallel()

	_, err := ParseSessionVerdict(`{"verdicts":[{"index":0,"score":0.5,"rationale":"r","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[]}]}`, 2)
	require.ErrorIs(t, err, analysis.ErrModelFailure)
}

func TestParseSessionVerdictRejectsDuplicateOrOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	_, err := ParseSessionVerdict(`{"verdicts":[
		{"index":0,"score":0.5,"rationale":"r","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[]},
		{"index":0,"score":0.5,"rationale":"r","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[]}
	]}`, 2)
	require.ErrorIs(t, err, analysis.ErrModelFailure)

	_, err = ParseSessionVerdict(`{"verdicts":[
		{"index":5,"score":0.5,"rationale":"r","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[]}
	]}`, 1)
	require.ErrorIs(t, err, analysis.ErrModelFailure)
}

func TestParseSessionVerdictRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := ParseSessionVerdict(`{"verdicts":[],"extra":true}`, 0)
	require.ErrorIs(t, err, analysis.ErrModelFailure)
}

func TestVerdictNormalizeClampsAndFilters(t *testing.T) {
	t.Parallel()

	negativeTurns := -3.0
	badConfidence := "certain"
	normalized, err := Verdict{
		Score:           1.7,
		Rationale:       strings.Repeat("長", 250),
		EstTurnsSaved:   &negativeTurns,
		EstMinutesSaved: nil,
		ROIConfidence:   &badConfidence,
		Flags:           []string{"harmful", "made-up", "harmful"},
	}.Normalize()

	require.NoError(t, err)
	require.InDelta(t, 1.0, normalized.Score, 0)
	require.Equal(t, 200, utf8.RuneCountInString(normalized.Rationale))
	require.Nil(t, normalized.EstTurnsSaved)
	require.Nil(t, normalized.ROIConfidence)
	require.Equal(t, []string{"harmful"}, normalized.Flags)
}

func TestVerdictNormalizeRejectsNonFiniteScore(t *testing.T) {
	t.Parallel()

	_, err := Verdict{
		Score:           math.NaN(),
		Rationale:       "r",
		EstTurnsSaved:   nil,
		EstMinutesSaved: nil,
		ROIConfidence:   nil,
		Flags:           nil,
	}.Normalize()
	require.ErrorIs(t, err, analysis.ErrModelFailure)
}
