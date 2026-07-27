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
