package suggest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

const runbook = `---
name: deploy-runbook
---

Announce the window.
Check the error budget.
Watch the canary.
Close the window.
`

const rollbackReference = `# Rollback

Revert the deploy.
Notify the channel.
`

func runbookTree() skilldiff.Tree {
	return skilldiff.Tree{Manifest: runbook, Files: nil}
}

func runbookTreeWithFiles() skilldiff.Tree {
	return skilldiff.Tree{
		Manifest: runbook,
		Files: []skilltree.File{
			{Path: "assets/diagram.png", Content: []byte{0x89, 'P', 'N', 'G', 0, 1}, ContentType: "image/png"},
			{Path: "references/rollback.md", Content: []byte(rollbackReference), ContentType: ""},
			{Path: "scripts/deploy.sh", Content: []byte("#!/bin/sh\necho deploy\n"), ContentType: ""},
		},
	}
}

func TestResolveChanges_KeepsEachEditInItsOwnDiff(t *testing.T) {
	t.Parallel()

	tree, resolved, err := ResolveChanges(runbookTree(), []GeneratedChange{
		{Path: "", Find: "Check the error budget.", Replace: "Check the error budget and page the on-call.", Rationale: "budget", Evidence: []int{1}},
		{Path: "", Find: "Close the window.", Replace: "Close the window and record the duration.", Rationale: "duration", Evidence: []int{2}},
	})
	require.NoError(t, err)
	require.Contains(t, tree.Manifest, "page the on-call")
	require.Contains(t, tree.Manifest, "record the duration")

	require.Len(t, resolved, 2)
	require.Empty(t, resolved[0].Path)
	require.Contains(t, resolved[0].Diff, "--- a/SKILL.md")
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

	_, _, err := ResolveChanges(runbookTree(), []GeneratedChange{
		{Path: "", Find: "Page the on-call.", Replace: "x", Rationale: "r", Evidence: nil},
	})
	require.ErrorContains(t, err, "does not appear in SKILL.md")

	_, _, err = ResolveChanges(runbookTree(), []GeneratedChange{
		{Path: "", Find: "the", Replace: "x", Rationale: "r", Evidence: nil},
	})
	require.ErrorContains(t, err, "appears more than once")
}

func TestResolveChanges_SkipsAnEditThatChangesNothing(t *testing.T) {
	t.Parallel()

	tree, resolved, err := ResolveChanges(runbookTree(), []GeneratedChange{
		{Path: "", Find: "Watch the canary.", Replace: "Watch the canary.", Rationale: "r", Evidence: nil},
	})
	require.NoError(t, err)
	require.Equal(t, runbook, tree.Manifest)
	require.Empty(t, resolved)
}

func TestResolveChanges_EditsAReferenceFile(t *testing.T) {
	t.Parallel()

	base := runbookTreeWithFiles()
	tree, resolved, err := ResolveChanges(base, []GeneratedChange{
		{Path: "references/rollback.md", Find: "Notify the channel.", Replace: "Notify the channel and the on-call.", Rationale: "notify", Evidence: nil},
		{Path: "SKILL.md", Find: "Watch the canary.", Replace: "Watch the canary for ten minutes.", Rationale: "canary", Evidence: nil},
	})
	require.NoError(t, err)

	require.Len(t, resolved, 2)
	require.Equal(t, "references/rollback.md", resolved[0].Path)
	require.Contains(t, resolved[0].Diff, "--- a/references/rollback.md\n+++ b/references/rollback.md\n")
	require.Empty(t, resolved[1].Path, "SKILL.md changes are stored without a path")

	reference, err := tree.Text("references/rollback.md")
	require.NoError(t, err)
	require.Contains(t, reference, "Notify the channel and the on-call.")
	require.Contains(t, tree.Manifest, "ten minutes")

	// The binary asset and the script ride along untouched, and the base tree
	// is not modified in place.
	require.Equal(t, base.Files[0], tree.Files[0])
	require.Equal(t, base.Files[2], tree.Files[2])
	original, err := base.Text("references/rollback.md")
	require.NoError(t, err)
	require.Equal(t, rollbackReference, original)
}

func TestResolveChanges_RejectsFilesItMayNotEdit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		find    string
		message string
	}{
		{name: "binary", path: "assets/diagram.png", find: "PNG", message: "not an editable text file"},
		{name: "missing", path: "references/missing.md", find: "x", message: "not an editable text file"},
		{name: "script", path: "scripts/deploy.sh", find: "echo deploy", message: "read-only"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ResolveChanges(runbookTreeWithFiles(), []GeneratedChange{
				{Path: tc.path, Find: tc.find, Replace: "y", Rationale: "r", Evidence: nil},
			})
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestValidateGeneration_NormalizesManifestPath(t *testing.T) {
	t.Parallel()

	generation := Generation{
		Decision: DecisionPropose,
		Changes: []GeneratedChange{
			{Path: " SKILL.md ", Find: "a", Replace: "b", Rationale: "r", Evidence: nil},
			{Path: "references/rollback.md", Find: "a", Replace: "b", Rationale: "r", Evidence: nil},
		},
		Rationale: "r",
	}
	require.NoError(t, ValidateGeneration(&generation, 0))
	require.Empty(t, generation.Changes[0].Path)
	require.Equal(t, "references/rollback.md", generation.Changes[1].Path)
}
