package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

func transcript() efficacy.Transcript {
	return efficacy.Transcript{Messages: []efficacy.TranscriptMessage{
		{Index: 1, CreatedAt: "2026-01-01T00:00:01Z", Role: "user", Content: "Do the task."},
		{Index: 2, CreatedAt: "2026-01-01T00:00:02Z", Role: "assistant", Content: "I attempted it."},
		{Index: 3, CreatedAt: "2026-01-01T00:00:03Z", Role: "user", Content: "The required check is missing."},
	}}
}

func expectedLabel() expectedRecommendation {
	return expectedRecommendation{
		AcceptableOutcomes:     []skills.FeedbackOutcome{skills.FeedbackOutcomeDidNotHelp},
		PersistenceEligible:    true,
		AcceptableIssueTypes:   []string{"requirement_omitted"},
		AcceptableChangeTypes:  []string{"reinforce_existing_requirement"},
		AllowedEvidenceIndices: []int{2, 3},
		RequiredEvidenceGroups: [][]int{{2, 3}},
	}
}

func actualLabel() actualRecommendation {
	return actualRecommendation{
		Outcome:                "did_not_help",
		Confidence:             "high",
		IssueType:              "requirement_omitted",
		ChangeType:             "reinforce_existing_requirement",
		EvidenceMessageIndices: []int{3},
	}
}

func fields(name string) caseFields {
	return caseFields{
		SkillName: name, SkillContent: "Follow the required check.", Surface: skills.FeedbackSourceAssistant,
		ActivatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ScoreMin: 0, ScoreMax: 0.5, Note: "private reviewer note",
	}
}

func validTestBenchSet() benchSet {
	return benchSet{
		JudgePromptVersion: efficacy.JudgePromptVersion, JudgeModel: efficacy.JudgeModel,
		MinimumAgreement: 0.8, RecommendationSchemaVersion: 3, MinimumRecommendationAgreement: 0.8,
		Cases: []testCase{
			{caseFields: fields("positive"), ID: "positive", Transcript: transcript(), ExpectedRecommendations: []expectedRecommendation{expectedLabel()}},
			{caseFields: fields("zero"), ID: "zero", Transcript: transcript(), ExpectedRecommendations: []expectedRecommendation{}},
		},
		RecommendationPairs: []recommendationPair{{
			caseFields: fields("pair"), ID: "pair", Transcript: transcript(), ExpectedRecommendation: expectedLabel(),
			BPrescription: efficacy.TranscriptMessage{Index: 4, CreatedAt: "2026-01-01T00:00:04Z", Role: "user", Content: "Always run the required check."},
		}},
	}
}

func writeBenchSet(t *testing.T, set benchSet) string {
	t.Helper()
	b, err := json.Marshal(set)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cases.json")
	require.NoError(t, os.WriteFile(path, b, 0o600))
	return path
}

func TestCommittedBenchSetV11InvariantsAndLabels(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("cases.json")
	require.NoError(t, err)
	require.NotContains(t, string(b), "note_concepts")

	var raw benchSet
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Equal(t, "v11", raw.JudgePromptVersion)
	require.Equal(t, 3, raw.RecommendationSchemaVersion)
	require.Len(t, raw.Cases, 10)
	require.Len(t, raw.RecommendationPairs, 5)

	type label struct {
		issue, change string
		allowed       []int
		groups        [][]int
	}
	want := map[string]label{
		"slight-help-error-context":           {"requirement_omitted", "reinforce_existing_requirement", []int{2, 3}, [][]int{{2, 3}}},
		"slight-help-hostile-skill":           {"priority_violated", "reinforce_existing_priority", []int{2, 3}, [][]int{{2}, {3}}},
		"moderate-help-partial-tests":         {"requirement_omitted", "reinforce_existing_requirement", []int{2, 3, 4}, [][]int{{3}}},
		"chat-release-native-formatting":      {"guidance_gap", "add_missing_requirement", []int{2, 3}, [][]int{{3}}},
		"pr-forbidden-validation-section":     {"prohibition_violated", "reinforce_existing_prohibition", []int{2, 3, 4, 5}, [][]int{{4, 5}}},
		"visual-pr-missing-proof":             {"requirement_omitted", "reinforce_existing_requirement", []int{2, 3}, [][]int{{3}}},
		"rigid-plan-checkpoints-cause-rework": {"harmful_overconstraint", "relax_constraint", []int{2, 3, 4}, [][]int{{2}, {3, 4}}},
		"skill-authoring-legacy-location":     {"obsolete_guidance", "replace_obsolete_guidance", []int{2, 3, 4}, [][]int{{2}, {3, 4}}},
	}

	labels := map[string]expectedRecommendation{}
	transcripts := map[string]efficacy.Transcript{}
	for _, tc := range raw.Cases {
		if len(tc.ExpectedRecommendations) == 1 {
			labels[tc.ID] = tc.ExpectedRecommendations[0]
			transcripts[tc.ID] = tc.Transcript
		}
	}
	for _, pair := range raw.RecommendationPairs {
		labels[pair.ID] = pair.ExpectedRecommendation
		transcripts[pair.ID] = pair.Transcript
	}
	require.Len(t, labels, 8)
	for id, expected := range want {
		got := labels[id]
		require.Equal(t, []string{expected.issue}, got.AcceptableIssueTypes, id)
		require.Equal(t, []string{expected.change}, got.AcceptableChangeTypes, id)
		require.Equal(t, expected.allowed, got.AllowedEvidenceIndices, id)
		require.Equal(t, expected.groups, got.RequiredEvidenceGroups, id)
		require.NotEmpty(t, got.AcceptableOutcomes, id)
		for _, index := range got.AllowedEvidenceIndices {
			require.True(t, slices.ContainsFunc(transcripts[id].Messages, func(message efficacy.TranscriptMessage) bool { return message.Index == index }), id)
		}
	}

	if efficacy.JudgePromptVersion != "v11" {
		t.Skip("production v11 prompt change has not landed in this worktree")
	}
	loaded, err := loadBenchSet("cases.json")
	require.NoError(t, err)
	positive, zero, pairs, scores := 0, 0, map[string]bool{}, 0
	for _, tc := range loaded.Cases {
		if len(tc.ExpectedRecommendations) == 0 {
			zero++
		} else {
			positive++
		}
		if tc.PairID != "" {
			pairs[tc.PairID] = true
		} else {
			scores++
		}
	}
	require.Equal(t, 8, positive)
	require.Equal(t, 12, zero)
	require.Len(t, pairs, 5)
	require.Equal(t, 10, scores)
}

func TestGradeStructuredRecommendationsRequiresEveryExactField(t *testing.T) {
	t.Parallel()
	want := []expectedRecommendation{expectedLabel()}
	correct := actualLabel()
	require.True(t, gradeStructuredRecommendations(want, []actualRecommendation{correct}).ExactOK)

	tests := []struct {
		name   string
		mutate func(*actualRecommendation)
		field  func(recommendationGrade) bool
	}{
		{"wrong outcome", func(g *actualRecommendation) { g.Outcome = "harmful" }, func(g recommendationGrade) bool { return g.OutcomeOK }},
		{"wrong confidence", func(g *actualRecommendation) { g.Confidence = "med" }, func(g recommendationGrade) bool { return g.PersistenceOK }},
		{"wrong issue", func(g *actualRecommendation) { g.IssueType = "priority_violated" }, func(g recommendationGrade) bool { return g.IssueOK }},
		{"wrong change", func(g *actualRecommendation) { g.ChangeType = "relax_constraint" }, func(g recommendationGrade) bool { return g.ChangeOK }},
		{"outside allowed evidence", func(g *actualRecommendation) { g.EvidenceMessageIndices = []int{4} }, func(g recommendationGrade) bool { return g.EvidenceOK }},
		{"duplicate evidence", func(g *actualRecommendation) { g.EvidenceMessageIndices = []int{3, 3} }, func(g recommendationGrade) bool { return g.EvidenceOK }},
		{"unsorted evidence", func(g *actualRecommendation) { g.EvidenceMessageIndices = []int{3, 2} }, func(g recommendationGrade) bool { return g.EvidenceOK }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := correct
			tt.mutate(&got)
			grade := gradeStructuredRecommendations(want, []actualRecommendation{got})
			require.False(t, tt.field(grade))
			require.False(t, grade.ExactOK)
		})
	}
	require.False(t, gradeStructuredRecommendations(want, nil).ExactOK)
	require.False(t, gradeStructuredRecommendations(want, []actualRecommendation{correct, correct}).ExactOK)
}

func TestGradeStructuredRecommendationsMatchesOneToOne(t *testing.T) {
	t.Parallel()

	want := expectedLabel()
	grade := gradeStructuredRecommendations(
		[]expectedRecommendation{want, want},
		[]actualRecommendation{actualLabel()},
	)
	require.Equal(t, 1, grade.MatchedCount)
	require.Equal(t, 1, grade.UnmatchedExpected)
	require.Equal(t, 0, grade.UnmatchedActual)
	require.False(t, grade.ExactOK)
}

func TestEvidenceGroupsRequireAllGroupsAndAcceptReviewedAlternative(t *testing.T) {
	t.Parallel()
	want := expectedLabel()
	want.AllowedEvidenceIndices = []int{2, 3, 4}
	want.RequiredEvidenceGroups = [][]int{{2}, {3, 4}}
	got := actualLabel()

	got.EvidenceMessageIndices = []int{2, 4}
	grade := gradeStructuredRecommendations([]expectedRecommendation{want}, []actualRecommendation{got})
	require.True(t, grade.EvidenceOK)
	require.Equal(t, 2, grade.EvidenceGroupsMatched)

	got.EvidenceMessageIndices = []int{2}
	grade = gradeStructuredRecommendations([]expectedRecommendation{want}, []actualRecommendation{got})
	require.False(t, grade.EvidenceOK)
	require.Equal(t, 1, grade.EvidenceGroupsMatched)
}

func TestRecommendationNoteWordingIsIrrelevant(t *testing.T) {
	t.Parallel()
	want := []expectedRecommendation{expectedLabel()}
	first, second := actualLabel(), actualLabel()
	first.Note = "The requirement was omitted."
	second.Note = "The requirement was definitely not omitted."
	require.Equal(t, gradeStructuredRecommendations(want, []actualRecommendation{first}), gradeStructuredRecommendations(want, []actualRecommendation{second}))
}

func TestLoadBenchSetRejectsInvalidStructuredEvidenceAndTaxonomy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*expectedRecommendation)
	}{
		{"no issue", func(e *expectedRecommendation) { e.AcceptableIssueTypes = nil }},
		{"multiple issues", func(e *expectedRecommendation) {
			e.AcceptableIssueTypes = []string{"requirement_omitted", "priority_violated"}
		}},
		{"unknown issue", func(e *expectedRecommendation) { e.AcceptableIssueTypes = []string{"other"} }},
		{"wrong change", func(e *expectedRecommendation) { e.AcceptableChangeTypes = []string{"other"} }},
		{"duplicate allowed", func(e *expectedRecommendation) { e.AllowedEvidenceIndices = []int{2, 2} }},
		{"unsorted allowed", func(e *expectedRecommendation) { e.AllowedEvidenceIndices = []int{3, 2} }},
		{"nonexistent allowed", func(e *expectedRecommendation) { e.AllowedEvidenceIndices = []int{2, 4} }},
		{"empty group", func(e *expectedRecommendation) { e.RequiredEvidenceGroups = [][]int{{}} }},
		{"duplicate group alternative", func(e *expectedRecommendation) { e.RequiredEvidenceGroups = [][]int{{2, 2}} }},
		{"unsorted group alternatives", func(e *expectedRecommendation) { e.RequiredEvidenceGroups = [][]int{{3, 2}} }},
		{"group outside allowed", func(e *expectedRecommendation) { e.RequiredEvidenceGroups = [][]int{{1}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			set := validTestBenchSet()
			tt.mutate(&set.Cases[0].ExpectedRecommendations[0])
			_, err := loadBenchSet(writeBenchSet(t, set))
			require.Error(t, err)
		})
	}
}

func TestLoadBenchSetStrictJSONSchemaVersionAndTranscriptValidation(t *testing.T) {
	t.Parallel()

	set := validTestBenchSet()
	set.RecommendationSchemaVersion = 2
	_, err := loadBenchSet(writeBenchSet(t, set))
	require.EqualError(t, err, "recommendation_schema_version must be 3")

	set = validTestBenchSet()
	set.Cases[0].Transcript.Messages[1].Index = 1
	_, err = loadBenchSet(writeBenchSet(t, set))
	require.ErrorContains(t, err, "message indices must be strictly increasing")

	b, err := json.Marshal(validTestBenchSet())
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cases.json")
	require.NoError(t, os.WriteFile(path, append(b, []byte("{}")...), 0o600))
	_, err = loadBenchSet(path)
	require.Error(t, err)
}

func TestStrictAttemptedRunQuorumAndABGatesRemainRequired(t *testing.T) {
	t.Parallel()
	set, err := loadBenchSet(writeBenchSet(t, validTestBenchSet()))
	require.NoError(t, err)
	results := make([]result, 0)
	for _, tc := range set.Cases {
		grade := gradeStructuredRecommendations(tc.ExpectedRecommendations, nil)
		results = append(results, result{RequestedModel: set.JudgeModel, CaseID: tc.ID, PairID: tc.PairID, PairVariant: tc.PairVariant, Phase: "recommendation_only", recommendationGrade: grade})
	}
	// One exact result plus two errors is not a strict majority of attempts.
	positiveID := set.Cases[0].ID
	good := gradeStructuredRecommendations(set.Cases[0].ExpectedRecommendations, []actualRecommendation{actualLabel()})
	results = append(results, result{RequestedModel: set.JudgeModel, CaseID: positiveID, recommendationGrade: good}, result{RequestedModel: set.JudgeModel, CaseID: positiveID, Error: "invalid_verdict"})
	summary := summarize(set, set.JudgeModel, results)
	require.Less(t, summary.PositiveAgreement, 1.0)
	require.False(t, summary.passes(set.MinimumAgreement, set.MinimumRecommendationAgreement))
}

func TestResultJSONContainsOnlySanitizedStructuredGrades(t *testing.T) {
	t.Parallel()
	sentinel := "PRIVATE_NOTE_SENTINEL"
	got := actualLabel()
	got.Note = sentinel
	grade := gradeStructuredRecommendations([]expectedRecommendation{expectedLabel()}, []actualRecommendation{got})
	res := result{RequestedModel: efficacy.JudgeModel, PromptVersion: "v11", RecommendationSchemaVersion: 3, CaseID: "safe-case", recommendationGrade: grade}
	b, err := json.Marshal(res)
	require.NoError(t, err)
	text := string(b)
	for _, forbidden := range []string{sentinel, "requirement_omitted", "reinforce_existing_requirement", "evidence_message_indices", "allowed_evidence_indices", "note", "transcript", "aliases"} {
		require.NotContains(t, text, forbidden)
	}
	for _, required := range []string{"issue_ok", "change_ok", "evidence_ok", "issue_correct_count", "change_correct_count", "evidence_correct_count"} {
		require.Contains(t, text, required)
	}
}

func TestLoadBaselineRejectsIncompatibleStructuredSchema(t *testing.T) {
	t.Parallel()
	rows := []result{{RequestedModel: efficacy.JudgeModel, PromptVersion: "v10", RecommendationSchemaVersion: 2}}
	b, err := json.Marshal(rows)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, os.WriteFile(path, b, 0o600))
	_, err = loadBaseline(path, 3, "")
	require.EqualError(t, err, "baseline recommendation schema version 2 is incompatible with requested version 3")
}

func TestCorpusContainsNoLegacyRecommendationFields(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("cases.json")
	require.NoError(t, err)
	for _, forbidden := range []string{"note_concepts", "any_of", "aliases"} {
		require.NotContains(t, string(b), forbidden)
	}
}
