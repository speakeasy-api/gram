package aitargets_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
)

func target(id string, enabled bool) aitargets.Target {
	return aitargets.Target{
		ID:          id,
		DisplayName: id,
		Category:    aitargets.CategoryHarness,
		Signatures:  aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{id}, ConfigDirs: []string{}, ProcessNames: []string{}},
		VersionHint: nil,
		Enabled:     enabled,
	}
}

func organizationRow(id string, enabled bool) aitargets.Entry {
	return aitargets.Entry{
		Target:     target(id, enabled),
		Source:     aitargets.SourceOrganization,
		Customized: false,
		CreatedAt:  time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}
}

func TestOverlayReplacesDefaultsAndAppendsAdditions(t *testing.T) {
	t.Parallel()
	defaults := []aitargets.Target{target("aider", true), target("codex", true)}
	rows := []aitargets.Entry{organizationRow("codex", false), organizationRow("acme-tool", true)}

	entries := aitargets.Overlay(defaults, rows)

	require.Len(t, entries, 3)
	require.Equal(t, "acme-tool", entries[0].ID, "entries are ordered by id")
	require.Equal(t, aitargets.SourceOrganization, entries[0].Source)
	require.False(t, entries[0].Customized)

	require.Equal(t, "aider", entries[1].ID)
	require.Equal(t, aitargets.SourceDefault, entries[1].Source)
	require.False(t, entries[1].Customized)
	require.True(t, entries[1].CreatedAt.IsZero(), "an untouched default has no row timestamps")

	require.Equal(t, "codex", entries[2].ID)
	require.Equal(t, aitargets.SourceDefault, entries[2].Source, "a row under a default id customizes that default")
	require.True(t, entries[2].Customized)
	require.False(t, entries[2].Enabled)
	require.False(t, entries[2].CreatedAt.IsZero())

	served := aitargets.Served(entries)
	require.Equal(t, []string{"acme-tool", "aider"}, []string{served[0].ID, served[1].ID})
	require.Len(t, served, 2, "a disabled entry is not served")
}

func TestOverlayLeavesTheDefaultsUntouched(t *testing.T) {
	t.Parallel()
	entries := aitargets.Overlay(aitargets.Defaults(), nil)
	require.Len(t, entries, len(aitargets.Defaults()))
	for _, entry := range entries {
		require.Equal(t, aitargets.SourceDefault, entry.Source)
		require.False(t, entry.Customized)
		require.True(t, entry.Enabled)
	}
	require.NoError(t, aitargets.ValidateServed(aitargets.Served(entries)))
}

// The reason the definition columns are null for a built-in: an organization
// that has switched one off, or decided about it, must still receive the next
// revision of that built-in. Storing the definition alongside the choice would
// freeze it at whatever the registry said on the day somebody clicked.
func TestOverlayKeepsTheBuiltInDefinitionAuthoritative(t *testing.T) {
	t.Parallel()

	// What the registry says today.
	current := target("aider", true)
	current.DisplayName = "Aider (renamed)"
	current.Signatures.Binaries = []string{"aider", "aider-chat"}

	// What the organization's row holds: its choices, and a definition that is
	// either absent or stale. Either way it must not win.
	row := organizationRow("aider", false)
	row.DisplayName = "Aider"
	row.Signatures.Binaries = []string{"aider"}
	row.Decision = aitargets.DecisionRecord{TargetID: "aider", Decision: aitargets.DecisionBlocked, Rationale: "not reviewed"}

	entries := aitargets.Overlay([]aitargets.Target{current}, []aitargets.Entry{row})
	require.Len(t, entries, 1)

	require.Equal(t, "Aider (renamed)", entries[0].DisplayName, "the built-in's definition wins")
	require.Equal(t, []string{"aider", "aider-chat"}, entries[0].Signatures.Binaries)

	require.False(t, entries[0].Enabled, "the organization's choice still wins")
	require.True(t, entries[0].Customized)
	require.Equal(t, aitargets.DecisionBlocked, entries[0].Decision.Decision, "and so does its decision")
	require.Equal(t, "not reviewed", entries[0].Decision.Rationale)
}

// The served version names the compiled-in defaults revision and nothing
// else. An organization's edits change the served targets, which the
// snapshot ETag hashes; that is what makes an agent re-apply the list.
func TestListVersionIsTheDefaultsRevision(t *testing.T) {
	t.Parallel()
	require.Equal(t, aitargets.DefaultsVersion, aitargets.ListVersion())
}
