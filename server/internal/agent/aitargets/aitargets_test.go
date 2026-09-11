package aitargets_test

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
)

func TestDefaultsAreValid(t *testing.T) {
	t.Parallel()

	defaults := aitargets.Defaults()
	require.Len(t, defaults, 11)
	require.NoError(t, aitargets.Validate(defaults))
	for _, target := range defaults {
		require.True(t, target.Enabled, "default %q must be enabled", target.ID)
	}
}

func TestDefaultsReturnsACopy(t *testing.T) {
	t.Parallel()

	first := aitargets.Defaults()
	first[0].DisplayName = "mutated"
	first[1].Signatures.Binaries[0] = "mutated"

	fresh := aitargets.Defaults()
	require.NotEqual(t, "mutated", fresh[0].DisplayName)
	require.NotEqual(t, "mutated", fresh[1].Signatures.Binaries[0])
}

func TestSnapshotSortsResolvesAndCopies(t *testing.T) {
	t.Parallel()

	defaults := aitargets.Defaults()
	reversed := slices.Clone(defaults)
	slices.Reverse(reversed)
	snapshot := aitargets.NewSnapshot(7, reversed)
	require.EqualValues(t, 7, snapshot.ListVersion)

	targets := snapshot.Targets()
	for i := 1; i < len(targets); i++ {
		require.Less(t, targets[i-1].ID, targets[i].ID, "targets must be ordered by id")
	}

	cursor, ok := snapshot.ByID("cursor")
	require.True(t, ok)
	require.Equal(t, "Cursor", cursor.DisplayName)
	cursor.Signatures.BundleIDs[0] = "mutated"
	again, _ := snapshot.ByID("cursor")
	require.NotEqual(t, "mutated", again.Signatures.BundleIDs[0], "ByID must hand out copies")

	_, ok = snapshot.ByID("definitely-not-a-target")
	require.False(t, ok)

	targets[0].DisplayName = "mutated"
	require.NotEqual(t, "mutated", snapshot.Targets()[0].DisplayName, "Targets must hand out copies")
}

func TestSnapshotETagTracksVersionAndContent(t *testing.T) {
	t.Parallel()

	defaults := aitargets.Defaults()
	base := aitargets.NewSnapshot(1, defaults)
	require.Equal(t, base.ETag, aitargets.NewSnapshot(1, defaults).ETag, "same input must fingerprint identically")
	require.NotEqual(t, base.ETag, aitargets.NewSnapshot(2, defaults).ETag, "a version bump must move the etag")

	renamed := aitargets.Defaults()
	renamed[0].DisplayName = "Renamed"
	require.NotEqual(t, base.ETag, aitargets.NewSnapshot(1, renamed).ETag, "a content change must move the etag")
}

// TestEnvelopeWireShape pins the JSON the device agent decodes.
func TestEnvelopeWireShape(t *testing.T) {
	t.Parallel()

	snapshot := aitargets.NewSnapshot(3, []aitargets.Target{{
		ID:          "cursor",
		DisplayName: "Cursor",
		Category:    aitargets.CategoryHarness,
		Signatures: aitargets.Signatures{
			BundleIDs:    []string{"com.todesktop.230313mzl4w4u92"},
			Binaries:     []string{"cursor"},
			ConfigDirs:   []string{"~/.cursor"},
			ProcessNames: []string{"Cursor"},
		},
		VersionHint: &aitargets.VersionHint{PlistKey: "CFBundleShortVersionString"},
		Enabled:     true,
	}})

	data, err := json.Marshal(snapshot.Envelope())
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, []string{"etag", "list_version", "schema_version", "targets"}, slices.Sorted(maps.Keys(decoded)))
	require.EqualValues(t, aitargets.SchemaVersion, decoded["schema_version"])
	require.EqualValues(t, 3, decoded["list_version"])
	require.Equal(t, snapshot.ETag, decoded["etag"])

	targets, ok := decoded["targets"].([]any)
	require.True(t, ok)
	require.Len(t, targets, 1)
	target, ok := targets[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []string{"category", "display_name", "enabled", "id", "signatures", "version_hint"}, slices.Sorted(maps.Keys(target)))
	signatures, ok := target["signatures"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []string{"binaries", "bundle_ids", "config_dirs", "process_names"}, slices.Sorted(maps.Keys(signatures)))
	require.Equal(t, map[string]any{"plist_key": "CFBundleShortVersionString"}, target["version_hint"])
}

func TestEnvelopeOmitsAbsentVersionHint(t *testing.T) {
	t.Parallel()

	snapshot := aitargets.NewSnapshot(1, aitargets.Defaults()[:1])
	data, err := json.Marshal(snapshot.Envelope())
	require.NoError(t, err)
	require.NotContains(t, string(data), "version_hint")
}
