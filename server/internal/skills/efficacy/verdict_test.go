package efficacy

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
)

func testVerdictTranscript() Transcript {
	return Transcript{Messages: []TranscriptMessage{{Index: 2}, {Index: 4}, {Index: 7}}}
}

func TestParseVerdictNormalizesModelOutput(t *testing.T) {
	t.Parallel()

	got, err := ParseVerdict(`
		{"score":0.75,"rationale":" the agent followed the skill ","est_turns_saved":2,
		 "est_minutes_saved":7.5,"roi_confidence":"med","flags":["partially_followed"],
		 "recommendations":[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2,4],"outcome":"partially_helped","note":"The skill omitted the final verification step.","confidence":"high"}]}
	`, testVerdictTranscript())

	require.NoError(t, err)
	require.InDelta(t, 0.75, got.Score, 0)
	require.Equal(t, "the agent followed the skill", got.Rationale)
	require.NotNil(t, got.EstTurnsSaved)
	require.InDelta(t, 2.0, *got.EstTurnsSaved, 0)
	require.NotNil(t, got.EstMinutesSaved)
	require.InDelta(t, 7.5, *got.EstMinutesSaved, 0)
	require.Equal(t, new("med"), got.ROIConfidence)
	require.Equal(t, []string{"partially_followed"}, got.Flags)
	require.Equal(t, []RawRecommendation{{
		IssueType:              "requirement_omitted",
		ChangeType:             "reinforce_existing_requirement",
		EvidenceMessageIndices: []int{2, 4},
		Outcome:                "partially_helped",
		Note:                   "The skill omitted the final verification step.",
		Confidence:             "high",
	}}, got.Recommendations)
}

func TestParseVerdictRejectsUnparseableOutput(t *testing.T) {
	t.Parallel()

	_, err := ParseVerdict("not json", Transcript{})

	require.ErrorIs(t, err, ErrModelFailure)
}

func TestParseVerdictRequiresTheStructuredOutputShape(t *testing.T) {
	t.Parallel()

	invalid := []string{
		`{}`,
		`null`,
		`{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"recommendations":[]}`,
		`{"score":null,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[],"recommendations":[]}`,
		`{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[],"recommendations":[],"extra":true}`,
	}
	for _, raw := range invalid {
		_, err := ParseVerdict(raw, Transcript{})
		require.ErrorIs(t, err, ErrModelFailure)
	}
}

func TestParseVerdictAcceptsExplicitNullNullableFields(t *testing.T) {
	t.Parallel()

	got, err := ParseVerdict(`{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[],"recommendations":[]}`, Transcript{})
	require.NoError(t, err)
	require.InDelta(t, 0.5, got.Score, 0)
	require.Nil(t, got.EstTurnsSaved)
	require.Nil(t, got.EstMinutesSaved)
	require.Nil(t, got.ROIConfidence)
	require.Empty(t, got.Recommendations)
}

func TestParseVerdictRequiresStrictRecommendationShape(t *testing.T) {
	t.Parallel()

	invalidRecommendations := []string{
		`null`,
		`[null]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":"irrelevant"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":null,"confidence":"high"}]`,
		`[{"issue_type":"invented","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":"irrelevant","confidence":"high"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"invented","evidence_message_indices":[2],"outcome":"did_not_help","note":"irrelevant","confidence":"high"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"helped","note":"useful","confidence":"high"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":"   ","confidence":"high"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":"irrelevant","confidence":"certain"}]`,
		`[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":[2],"outcome":"did_not_help","note":"irrelevant","confidence":"high","extra":true}]`,
	}
	for _, recommendations := range invalidRecommendations {
		raw := `{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[],"recommendations":` + recommendations + `}`
		_, err := ParseVerdict(raw, testVerdictTranscript())
		require.ErrorIs(t, err, ErrModelFailure)
	}
}

func TestParseVerdictRejectsInvalidRecommendationEvidenceIndices(t *testing.T) {
	t.Parallel()

	for _, evidence := range []string{
		`[]`,
		`[0]`,
		`[-1]`,
		`[2,2]`,
		`[4,2]`,
		`[3]`,
		`["2"]`,
		`[2.5]`,
	} {
		recommendation := `[{"issue_type":"requirement_omitted","change_type":"reinforce_existing_requirement","evidence_message_indices":` + evidence + `,"outcome":"did_not_help","note":"irrelevant","confidence":"high"}]`
		raw := `{"score":0.5,"rationale":"ok","est_turns_saved":null,"est_minutes_saved":null,"roi_confidence":null,"flags":[],"recommendations":` + recommendation + `}`
		_, err := ParseVerdict(raw, testVerdictTranscript())
		require.ErrorIs(t, err, ErrModelFailure, evidence)
	}
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

func TestNormalizeVerdictNormalizesAndExactlyDeduplicatesRecommendations(t *testing.T) {
	t.Parallel()

	longNote := strings.Repeat("é", domainskills.MaxFeedbackNoteRunes+1)
	first := RawRecommendation{IssueType: "requirement_omitted", ChangeType: "reinforce_existing_requirement", EvidenceMessageIndices: []int{2}, Outcome: "did_not_help", Note: longNote, Confidence: "high"}
	differentEvidence := RawRecommendation{IssueType: "requirement_omitted", ChangeType: "reinforce_existing_requirement", EvidenceMessageIndices: []int{4}, Outcome: "did_not_help", Note: longNote, Confidence: "high"}
	differentNote := RawRecommendation{IssueType: "requirement_omitted", ChangeType: "reinforce_existing_requirement", EvidenceMessageIndices: []int{2}, Outcome: "did_not_help", Note: "different evidence", Confidence: "high"}
	differentConfidence := RawRecommendation{IssueType: "requirement_omitted", ChangeType: "reinforce_existing_requirement", EvidenceMessageIndices: []int{2}, Outcome: "did_not_help", Note: "different evidence", Confidence: "low"}
	got, err := (Verdict{Score: 0.5, Recommendations: []RawRecommendation{first, first, differentEvidence, differentNote, differentConfidence}}).Normalize()

	require.NoError(t, err)
	require.Len(t, got.Recommendations, 4)
	require.Equal(t, domainskills.MaxFeedbackNoteRunes, utf8.RuneCountInString(got.Recommendations[0].Note))
	require.True(t, utf8.ValidString(got.Recommendations[0].Note))
	require.Equal(t, []int{4}, got.Recommendations[1].EvidenceMessageIndices)
	require.Equal(t, differentNote, got.Recommendations[2])
	require.Equal(t, differentConfidence, got.Recommendations[3])
}

func TestNormalizeVerdictRejectsInvalidRecommendationDomains(t *testing.T) {
	t.Parallel()

	valid := RawRecommendation{IssueType: "requirement_omitted", ChangeType: "reinforce_existing_requirement", EvidenceMessageIndices: []int{2}, Outcome: "did_not_help", Note: "irrelevant", Confidence: "high"}
	for _, recommendation := range []RawRecommendation{
		{IssueType: "invented", ChangeType: valid.ChangeType, Outcome: valid.Outcome, Note: valid.Note, Confidence: valid.Confidence},
		{IssueType: valid.IssueType, ChangeType: "invented", Outcome: valid.Outcome, Note: valid.Note, Confidence: valid.Confidence},
		{IssueType: valid.IssueType, ChangeType: valid.ChangeType, Outcome: "helped", Note: valid.Note, Confidence: valid.Confidence},
		{IssueType: valid.IssueType, ChangeType: valid.ChangeType, Outcome: valid.Outcome, Note: valid.Note, Confidence: "certain"},
	} {
		_, err := (Verdict{Score: 0.5, Recommendations: []RawRecommendation{recommendation}}).Normalize()
		require.ErrorIs(t, err, ErrModelFailure)
	}
}

func TestNormalizeVerdictRejectsInvalidRecommendationEvidenceIndices(t *testing.T) {
	t.Parallel()

	valid := RawRecommendation{
		IssueType:              "requirement_omitted",
		ChangeType:             "reinforce_existing_requirement",
		EvidenceMessageIndices: []int{2},
		Outcome:                "did_not_help",
		Note:                   "irrelevant",
		Confidence:             "high",
	}
	for _, evidence := range [][]int{nil, {}, {0}, {-1}, {2, 2}, {4, 2}} {
		recommendation := valid
		recommendation.EvidenceMessageIndices = evidence
		_, err := (Verdict{Score: 0.5, Recommendations: []RawRecommendation{recommendation}}).Normalize()
		require.ErrorIs(t, err, ErrModelFailure, evidence)
	}
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
