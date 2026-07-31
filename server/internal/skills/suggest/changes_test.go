package suggest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const runbook = `---
name: deploy-runbook
---

Announce the window.
Check the error budget.
Watch the canary.
Close the window.
`

func TestResolveChanges_KeepsEachEditInItsOwnDiff(t *testing.T) {
	t.Parallel()

	content, resolved, err := ResolveChanges(runbook, []GeneratedChange{
		{Find: "Check the error budget.", Replace: "Check the error budget and page the on-call.", Rationale: "budget", Evidence: []int{1}},
		{Find: "Close the window.", Replace: "Close the window and record the duration.", Rationale: "duration", Evidence: []int{2}},
	})
	require.NoError(t, err)
	require.Contains(t, content, "page the on-call")
	require.Contains(t, content, "record the duration")

	require.Len(t, resolved, 2)
	require.Contains(t, resolved[0].Diff, "+Check the error budget and page the on-call.")
	require.NotContains(t, resolved[0].Diff, "+Close the window and record the duration.")
	require.Equal(t, []int{1}, resolved[0].Evidence)
	require.Contains(t, resolved[1].Diff, "+Close the window and record the duration.")
	// The first edit shows up only as context in the second diff, never as an
	// edit the second change would re-apply.
	require.NotContains(t, resolved[1].Diff, "+Check the error budget and page the on-call.")
	require.Equal(t, []int{2}, resolved[1].Evidence)
}

func TestResolveChanges_RejectsTextItCannotLocateExactlyOnce(t *testing.T) {
	t.Parallel()

	_, _, err := ResolveChanges(runbook, []GeneratedChange{
		{Find: "Page the on-call.", Replace: "x", Rationale: "r", Evidence: nil},
	})
	require.ErrorContains(t, err, "does not appear in the skill")

	_, _, err = ResolveChanges(runbook, []GeneratedChange{
		{Find: "the", Replace: "x", Rationale: "r", Evidence: nil},
	})
	require.ErrorContains(t, err, "appears more than once")
}

func TestResolveChanges_SkipsAnEditThatChangesNothing(t *testing.T) {
	t.Parallel()

	content, resolved, err := ResolveChanges(runbook, []GeneratedChange{
		{Find: "Watch the canary.", Replace: "Watch the canary.", Rationale: "r", Evidence: nil},
	})
	require.NoError(t, err)
	require.Equal(t, runbook, content)
	require.Empty(t, resolved)
}
