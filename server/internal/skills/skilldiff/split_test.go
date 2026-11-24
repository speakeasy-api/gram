package skilldiff_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

const apiReference = `# API reference

Authenticate with a bearer token.
-- Deprecated: basic auth.
Paginate with cursors.
Retry on 429 with backoff.
`

func TestUnifiedFile_NamesTheFileInItsHeaders(t *testing.T) {
	t.Parallel()

	diff, err := skilldiff.UnifiedFile("references/api.md", apiReference, strings.Replace(apiReference, "cursors", "opaque cursors", 1))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(diff, "--- a/references/api.md\n+++ b/references/api.md\n"), diff)

	manifest, err := skilldiff.Unified(incidentSkill, incidentSkill+"Close the review.\n")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(manifest, "--- a/SKILL.md\n+++ b/SKILL.md\n"), manifest)
}

func TestSplit_RoundTripsAMultiFileDiff(t *testing.T) {
	t.Parallel()

	proposedSkill := strings.Replace(incidentSkill, "List action items", "List dated action items", 1)
	// Removing a line that starts with "--" yields a "--- " diff line inside the
	// hunk, which must not be read as the next file's header.
	proposedReference := strings.Replace(apiReference, "-- Deprecated: basic auth.\n", "", 1)
	proposedReference = strings.Replace(proposedReference, "Retry on 429", "Retry on 429 and 503", 1)

	skillDiff, err := skilldiff.Unified(incidentSkill, proposedSkill)
	require.NoError(t, err)
	referenceDiff, err := skilldiff.UnifiedFile("references/api.md", apiReference, proposedReference)
	require.NoError(t, err)
	require.Contains(t, referenceDiff, "\n--- Deprecated: basic auth.\n")

	files, err := skilldiff.Split(skilldiff.Join([]skilldiff.FileDiff{
		{Path: skilltree.ManifestPath, Diff: skillDiff},
		{Path: "references/api.md", Diff: referenceDiff},
	}))
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Equal(t, skilltree.ManifestPath, files[0].Path)
	require.Equal(t, skillDiff, files[0].Diff)
	require.Equal(t, "references/api.md", files[1].Path)
	require.Equal(t, referenceDiff, files[1].Diff)

	appliedSkill, err := skilldiff.Apply(incidentSkill, files[0].Diff)
	require.NoError(t, err)
	require.Equal(t, proposedSkill, appliedSkill)
	appliedReference, err := skilldiff.Apply(apiReference, files[1].Diff)
	require.NoError(t, err)
	require.Equal(t, proposedReference, appliedReference)
}

func TestSplit_EmptyDiffHasNoFiles(t *testing.T) {
	t.Parallel()

	files, err := skilldiff.Split("  \n")
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestSplit_RejectsMalformedDiffs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		diff string
	}{
		{name: "rename", diff: "--- a/references/old.md\n+++ b/references/new.md\n@@ -1 +1 @@\n-a\n+b\n"},
		{name: "hunk before headers", diff: "@@ -1 +1 @@\n-a\n+b\n"},
		{name: "truncated hunk", diff: "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1,2 +1,2 @@\n-a\n+b\n"},
		{name: "text before headers", diff: "not a diff\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := skilldiff.Split(tc.diff)
			require.ErrorIs(t, err, skilldiff.ErrMalformed)
		})
	}
}

func TestTree_ApplyChangeEditsOnlyTheNamedTextFile(t *testing.T) {
	t.Parallel()

	logo := skilltree.File{Path: "assets/logo.png", Content: []byte{0x89, 'P', 'N', 'G', 0}, ContentType: "image/png"}
	base := skilldiff.Tree{
		Manifest: incidentSkill,
		Files: []skilltree.File{
			logo,
			{Path: "references/api.md", Content: []byte(apiReference), ContentType: "text/markdown; charset=utf-8"},
		},
	}
	proposed := strings.Replace(apiReference, "cursors", "opaque cursors", 1)
	diff, err := skilldiff.UnifiedFile("references/api.md", apiReference, proposed)
	require.NoError(t, err)

	applied, err := base.ApplyChange("references/api.md", diff)
	require.NoError(t, err)
	require.Equal(t, incidentSkill, applied.Manifest)
	require.Equal(t, logo, applied.Files[0])
	require.Equal(t, proposed, string(applied.Files[1].Content))
	require.Equal(t, "text/markdown; charset=utf-8", applied.Files[1].ContentType)
	require.Equal(t, apiReference, string(base.Files[1].Content), "the base tree is left untouched")

	_, err = base.ApplyChange("assets/logo.png", diff)
	require.ErrorIs(t, err, skilldiff.ErrConflict)
	_, err = base.ApplyChange("references/missing.md", diff)
	require.ErrorIs(t, err, skilldiff.ErrConflict)
}

func TestIsText(t *testing.T) {
	t.Parallel()

	require.True(t, skilldiff.IsText([]byte("plain text\n")))
	require.True(t, skilldiff.IsText(nil))
	require.False(t, skilldiff.IsText([]byte{'a', 0, 'b'}))
	require.False(t, skilldiff.IsText([]byte{0xff, 0xfe}))
}
