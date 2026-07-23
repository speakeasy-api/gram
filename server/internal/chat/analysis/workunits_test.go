package analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseWorkUnitsVerdict_Valid(t *testing.T) {
	t.Parallel()

	raw := `{
		"tasks": [
			{"id": 1, "request": "fix the bug", "band": "D", "base_units": 22, "modifier": 1.0, "completion": 1.0, "units": 22, "nearest_exemplar": "E20", "rationale": "root-caused and fixed with green tests"},
			{"id": 2, "request": "update the runbook", "band": "B", "base_units": 5, "modifier": 1.0, "completion": 0.5, "units": 3, "nearest_exemplar": "E9", "rationale": "half rewritten"}
		],
		"session_units": 25,
		"flags": []
	}`

	verdict, err := ParseWorkUnitsVerdict(raw)
	require.NoError(t, err)
	require.Len(t, verdict.Tasks, 2)
	require.InDelta(t, 22, verdict.Tasks[0].Units, 0.0001)
	require.InDelta(t, 25, verdict.SessionUnits, 0.0001)
	require.Empty(t, verdict.Flags)
}

func TestParseWorkUnitsVerdict_ClampsAndRecomputesTotal(t *testing.T) {
	t.Parallel()

	// Per-task units exceed the prompt's [-30, 100] clamp and the reported
	// session total disagrees with the tasks; both must be normalized.
	raw := `{
		"tasks": [
			{"id": 1, "request": "build everything", "band": "F", "base_units": 90, "modifier": 1.5, "completion": 1.0, "units": 135, "nearest_exemplar": "E36", "rationale": "capped"},
			{"id": 2, "request": "harmful incident", "band": "B", "base_units": 5, "modifier": 1.0, "completion": 0.0, "units": -50, "nearest_exemplar": "E37", "rationale": "harm"}
		],
		"session_units": 999,
		"flags": ["harm", "harm", "made-up-flag"]
	}`

	verdict, err := ParseWorkUnitsVerdict(raw)
	require.NoError(t, err)
	require.InDelta(t, 100, verdict.Tasks[0].Units, 0.0001)
	require.InDelta(t, -30, verdict.Tasks[1].Units, 0.0001)
	require.InDelta(t, 70, verdict.SessionUnits, 0.0001)
	require.Equal(t, []string{"harm"}, verdict.Flags)
}

func TestParseWorkUnitsVerdict_UnknownFieldIsModelFailure(t *testing.T) {
	t.Parallel()

	_, err := ParseWorkUnitsVerdict(`{"tasks": [], "session_units": 0, "flags": [], "extra": true}`)
	require.ErrorIs(t, err, ErrModelFailure)
}

func TestParseWorkUnitsVerdict_GarbageIsModelFailure(t *testing.T) {
	t.Parallel()

	_, err := ParseWorkUnitsVerdict("the session was great, ten points")
	require.ErrorIs(t, err, ErrModelFailure)
}

func TestParseWorkUnitsVerdict_TruncatesText(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("長", 500)
	raw := `{
		"tasks": [
			{"id": 1, "request": "` + long + `", "band": "A", "base_units": 1, "modifier": 1.0, "completion": 1.0, "units": 1, "nearest_exemplar": "E1", "rationale": "` + long + `"}
		],
		"session_units": 1,
		"flags": []
	}`

	verdict, err := ParseWorkUnitsVerdict(raw)
	require.NoError(t, err)
	require.Len(t, []rune(verdict.Tasks[0].Request), maxWorkUnitsTextRunes)
	require.Len(t, []rune(verdict.Tasks[0].Rationale), maxWorkUnitsTextRunes)
}

func TestWorkUnitsJudgeName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "work_units", WorkUnitsJudgeName)
	// The embedded system prompt must be the verbatim spec, not a placeholder.
	require.Contains(t, workUnitsSystemPrompt, "meaningful work units")
	require.Contains(t, workUnitsSystemPrompt, "Calibration exemplars")
}

func TestNewJudges_RejectsDuplicateAndInvalidNames(t *testing.T) {
	t.Parallel()

	a := stubNamedJudge{name: "work_units"}
	_, err := NewJudges(a, a)
	require.ErrorContains(t, err, "registered twice")

	_, err = NewJudges(stubNamedJudge{name: "Not Valid!"})
	require.ErrorContains(t, err, "must match")

	judges, err := NewJudges(a, stubNamedJudge{name: "resolution"})
	require.NoError(t, err)
	require.Equal(t, []string{"work_units", "resolution"}, judges.Names())
	_, ok := judges.Get("resolution")
	require.True(t, ok)
	_, ok = judges.Get("missing")
	require.False(t, ok)
}
