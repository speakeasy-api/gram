package tooleval

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegressionsNeedAPassingBaselineAndAFailingMajority(t *testing.T) {
	t.Parallel()

	baseline := Baseline{Model: "m", Cases: map[string]map[string]CaseResult{
		"steady":   {"direct": {Passes: 3, Runs: 3}},
		"broken":   {"direct": {Passes: 3, Runs: 3}, "progressive": {Passes: 2, Runs: 3}},
		"flaky":    {"direct": {Passes: 2, Runs: 3}},
		"was-bad":  {"direct": {Passes: 0, Runs: 3}},
		"untested": {},
	}}
	now := map[string]map[string]CaseResult{
		"steady":  {"direct": {Passes: 3, Runs: 3}},
		"broken":  {"direct": {Passes: 1, Runs: 3}, "progressive": {Passes: 0, Runs: 3}},
		"flaky":   {"direct": {Passes: 1, Runs: 2}},
		"was-bad": {"direct": {Passes: 0, Runs: 3}},
		"new":     {"direct": {Passes: 0, Runs: 3}},
	}
	regressions := Regressions(baseline, now)
	require.Equal(t, []Regression{
		{CaseID: "broken", Surface: "direct", Before: CaseResult{Passes: 3, Runs: 3}, After: CaseResult{Passes: 1, Runs: 3}},
		{CaseID: "broken", Surface: "progressive", Before: CaseResult{Passes: 2, Runs: 3}, After: CaseResult{Passes: 0, Runs: 3}},
	}, regressions, "a tie is not a failing majority, an old miss is not a regression, and a case with no baseline is not gated")
	require.Contains(t, RegressionsMarkdown(regressions), "| broken | direct | 3/3 | 1/3 |")
	require.Empty(t, RegressionsMarkdown(nil))
}

func TestBaselineMergeReplacesCoveredEntriesAndDropsRemovedCases(t *testing.T) {
	t.Parallel()

	base := Baseline{Model: "m", Cases: map[string]map[string]CaseResult{
		"kept":    {"direct": {Passes: 3, Runs: 3}, "progressive": {Passes: 1, Runs: 3}},
		"removed": {"direct": {Passes: 3, Runs: 3}},
	}}
	results := map[string]map[string]CaseResult{"kept": {"progressive": {Passes: 3, Runs: 3}}, "new": {"direct": {Passes: 2, Runs: 3}}}
	cases := []Case{reportCase("kept", []string{"get_mcp"}, nil), reportCase("new", []string{"get_mcp"}, nil)}

	merged := base.Merge("m", results, cases)
	require.Equal(t, map[string]map[string]CaseResult{
		"kept": {"direct": {Passes: 3, Runs: 3}, "progressive": {Passes: 3, Runs: 3}},
		"new":  {"direct": {Passes: 2, Runs: 3}},
	}, merged.Cases)

	other := base.Merge("other-model", results, cases)
	require.NotContains(t, other.Cases["kept"], "direct", "another model's baseline is not carried over")

	data, err := merged.Marshal()
	require.NoError(t, err)
	var decoded Baseline
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, merged, decoded)
}

func TestReportResultsCountsGradedRunsPerSurface(t *testing.T) {
	t.Parallel()

	read := reportCase("read", []string{"get_mcp"}, nil)
	hit := Outcome{Model: "m", Surface: "direct", Case: read, Selected: &Call{Tool: "get_mcp", Arguments: json.RawMessage(`{}`)}, Hit: true, SchemaValid: true}
	miss := Outcome{Model: "m", Surface: "direct", Case: read, Selected: &Call{Tool: "find_mcp", Arguments: json.RawMessage(`{}`)}}
	errored := Outcome{Model: "m", Surface: "direct", Case: read, Err: errTest}
	otherModel := hit
	otherModel.Model = "x"

	report := &Report{Outcomes: []Outcome{hit, miss, errored, otherModel}}
	require.Equal(t, map[string]map[string]CaseResult{"read": {"direct": {Passes: 1, Runs: 2}}}, report.Results("m"))
}

var errTest = errors.New("provider failed")
