package skilldiff_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

const incidentSkill = `---
name: incident-review
---

Write a blameless narrative: impact, detection, fix.
Produce a five-whys root-cause section.
List action items with named owners.
Attach the raw timeline as an appendix.
`

func TestUnified_RoundTripsThroughApply(t *testing.T) {
	t.Parallel()

	proposed := strings.Replace(
		incidentSkill,
		"Produce a five-whys",
		"Quantify impact: affected users, failed requests, and minutes of degradation.\nProduce a five-whys",
		1,
	)

	diff, err := skilldiff.Unified(incidentSkill, proposed)
	require.NoError(t, err)
	require.Contains(t, diff, "+Quantify impact:")

	applied, err := skilldiff.Apply(incidentSkill, diff)
	require.NoError(t, err)
	require.Equal(t, proposed, applied)
}

func TestApply_ReplaysOntoUnrelatedEdits(t *testing.T) {
	t.Parallel()

	proposed := strings.Replace(incidentSkill, "List action items with named owners.", "List action items with named owners and due dates.", 1)
	diff, err := skilldiff.Unified(incidentSkill, proposed)
	require.NoError(t, err)

	// A later version edits the frontmatter, far enough from the hunk that the
	// recorded context still matches.
	moved := strings.Replace(incidentSkill, "name: incident-review", "name: incident-review\ndescription: Review incidents.", 1)

	applied, err := skilldiff.Apply(moved, diff)
	require.NoError(t, err)
	require.Contains(t, applied, "named owners and due dates.")
	require.Contains(t, applied, "description: Review incidents.")
}

func TestApply_ConflictsWhenContextChanged(t *testing.T) {
	t.Parallel()

	proposed := strings.Replace(incidentSkill, "List action items with named owners.", "List action items with named owners and due dates.", 1)
	diff, err := skilldiff.Unified(incidentSkill, proposed)
	require.NoError(t, err)

	rewritten := strings.ReplaceAll(incidentSkill, "List action items with named owners.", "Assign every follow-up to a person.")

	_, err = skilldiff.Apply(rewritten, diff)
	require.ErrorIs(t, err, skilldiff.ErrConflict)
}

func TestApply_EmptyDiffIsIdentity(t *testing.T) {
	t.Parallel()

	diff, err := skilldiff.Unified(incidentSkill, incidentSkill)
	require.NoError(t, err)
	require.Empty(t, diff)

	applied, err := skilldiff.Apply(incidentSkill, diff)
	require.NoError(t, err)
	require.Equal(t, incidentSkill, applied)
}

func TestApply_RejectsUnparseableDiff(t *testing.T) {
	t.Parallel()

	_, err := skilldiff.Apply(incidentSkill, "not a diff at all")
	require.ErrorIs(t, err, skilldiff.ErrConflict)
}

func TestApply_NormalizesMissingTrailingNewline(t *testing.T) {
	t.Parallel()

	base := strings.TrimSuffix(incidentSkill, "\n")
	proposed := base + "\nKeep the appendix machine readable."

	diff, err := skilldiff.Unified(base, proposed)
	require.NoError(t, err)

	applied, err := skilldiff.Apply(base, diff)
	require.NoError(t, err)
	require.Equal(t, proposed+"\n", applied)
}

func TestApply_ReplaysEditAtEndOfSkill(t *testing.T) {
	t.Parallel()

	proposed := incidentSkill + "Keep the appendix machine readable.\n"

	diff, err := skilldiff.Unified(incidentSkill, proposed)
	require.NoError(t, err)

	applied, err := skilldiff.Apply(incidentSkill, diff)
	require.NoError(t, err)
	require.Equal(t, proposed, applied)
}

const runbookSkill = `---
name: deploy-runbook
---

## Before

Announce the window in the release channel.
Confirm the rollback artifact exists.
Check the error budget.

## During

Watch the canary for ten minutes.
Compare latency against the previous release.
Stop on any regression.

## After

Post the outcome in the release channel.
File follow-ups for anything deferred.
Close the window.
`

func TestHunks_SplitsEachChangeIntoAnIndependentDiff(t *testing.T) {
	t.Parallel()

	proposed := strings.NewReplacer(
		"Check the error budget.", "Check the error budget and page the on-call.",
		"Close the window.", "Close the window and record the duration.",
	).Replace(runbookSkill)

	diff, err := skilldiff.Unified(runbookSkill, proposed)
	require.NoError(t, err)

	hunks := skilldiff.Hunks(diff)
	require.Len(t, hunks, 2)

	first, err := skilldiff.Apply(runbookSkill, hunks[0])
	require.NoError(t, err)
	require.Contains(t, first, "page the on-call")
	require.NotContains(t, first, "record the duration")

	second, err := skilldiff.Apply(runbookSkill, hunks[1])
	require.NoError(t, err)
	require.NotContains(t, second, "page the on-call")
	require.Contains(t, second, "record the duration")
}

func TestHunks_RemainderStillAppliesAfterOneHunkLands(t *testing.T) {
	t.Parallel()

	proposed := strings.NewReplacer(
		"Check the error budget.", "Check the error budget and page the on-call.",
		"Close the window.", "Close the window and record the duration.",
	).Replace(runbookSkill)

	diff, err := skilldiff.Unified(runbookSkill, proposed)
	require.NoError(t, err)
	hunks := skilldiff.Hunks(diff)

	applied, err := skilldiff.Apply(runbookSkill, hunks[0])
	require.NoError(t, err)

	// What is left of the suggestion is the edit from the new base to the
	// original proposal, which no longer mentions the change already taken.
	remaining, err := skilldiff.Unified(applied, proposed)
	require.NoError(t, err)
	require.NotContains(t, remaining, "page the on-call")
	require.Contains(t, remaining, "+Close the window and record the duration.")
	require.Len(t, skilldiff.Hunks(remaining), 1)

	final, err := skilldiff.Apply(applied, remaining)
	require.NoError(t, err)
	require.Equal(t, proposed, final)
}

func TestHunks_EmptyDiffHasNoHunks(t *testing.T) {
	t.Parallel()

	require.Empty(t, skilldiff.Hunks(""))
	require.Empty(t, skilldiff.Hunks("   \n"))
}
