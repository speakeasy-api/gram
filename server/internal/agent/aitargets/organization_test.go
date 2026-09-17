package aitargets_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
)

func target(id string) aitargets.Target {
	return aitargets.Target{
		ID:          id,
		DisplayName: id,
		Category:    aitargets.CategoryHarness,
		Signatures:  aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{id}, ConfigDirs: []string{}, ProcessNames: []string{}},
		VersionHint: nil,
	}
}

func organizationRow(id string) aitargets.Entry {
	return aitargets.Entry{
		Target:     target(id),
		Source:     aitargets.SourceOrganization,
		Customized: false,
		CreatedAt:  time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
	}
}

func TestOverlayReplacesDefaultsAndAppendsAdditions(t *testing.T) {
	t.Parallel()
	defaults := []aitargets.Target{target("aider"), target("codex")}
	rows := []aitargets.Entry{organizationRow("codex"), organizationRow("acme-tool")}

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
	require.False(t, entries[2].CreatedAt.IsZero())

	// Everything in the inventory is served. A row under a built-in's id
	// records a decision about it; it is not a second switch that could hold
	// the target back from the agents.
	served := aitargets.Served(entries)
	require.Equal(t, []string{"acme-tool", "aider", "codex"}, []string{served[0].ID, served[1].ID, served[2].ID})
	require.Len(t, served, 3, "every entry is served")
}

func TestOverlayLeavesTheDefaultsUntouched(t *testing.T) {
	t.Parallel()
	entries := aitargets.Overlay(aitargets.Defaults(), nil)
	require.Len(t, entries, len(aitargets.Defaults()))
	for _, entry := range entries {
		require.Equal(t, aitargets.SourceDefault, entry.Source)
		require.False(t, entry.Customized)
	}
	require.NoError(t, aitargets.ValidateServed(aitargets.Served(entries)))
}

// The reason the definition columns are null for a built-in: an organization
// that has decided about one must still receive the next revision of that
// built-in. Storing the definition alongside the decision would freeze it at
// whatever the registry said on the day somebody clicked.
func TestOverlayKeepsTheBuiltInDefinitionAuthoritative(t *testing.T) {
	t.Parallel()

	// What the registry says today.
	current := target("aider")
	current.DisplayName = "Aider (renamed)"
	current.Signatures.Binaries = []string{"aider", "aider-chat"}

	// What the organization's row holds: its decision, and a definition that is
	// either absent or stale. Either way it must not win.
	row := organizationRow("aider")
	row.DisplayName = "Aider"
	row.Signatures.Binaries = []string{"aider"}
	row.Decision = aitargets.DecisionRecord{TargetID: "aider", Decision: aitargets.DecisionBlocked, Rationale: "not reviewed"}

	entries := aitargets.Overlay([]aitargets.Target{current}, []aitargets.Entry{row})
	require.Len(t, entries, 1)

	require.Equal(t, "Aider (renamed)", entries[0].DisplayName, "the built-in's definition wins")
	require.Equal(t, []string{"aider", "aider-chat"}, entries[0].Signatures.Binaries)

	require.True(t, entries[0].Customized)
	require.Equal(t, aitargets.DecisionBlocked, entries[0].Decision.Decision, "the organization's decision still wins")
	require.Equal(t, "not reviewed", entries[0].Decision.Rationale)
}

// The served version names the compiled-in defaults revision and nothing
// else. An organization's edits change the served targets, which the
// snapshot ETag hashes; that is what makes an agent re-apply the list.
func TestListVersionIsTheDefaultsRevision(t *testing.T) {
	t.Parallel()
	require.Equal(t, aitargets.DefaultsVersion, aitargets.ListVersion())
}

// ChatGPT ships as two desktop builds that a device tells apart by bundle id
// and nothing else: Classic kept com.openai.chat, the identifier the app first
// shipped under, and the current app moved to com.openai.codex. Both ids are
// asserted here rather than left to the registry, because dropping either one
// makes that build invisible on the device while the other still reports, and
// the resulting inventory looks correct.
func TestBothChatGPTBuildsAreDetectableInstalledAndRunning(t *testing.T) {
	t.Parallel()

	byID := func(id string) *aitargets.Target {
		for _, entry := range aitargets.Defaults() {
			if entry.ID == id {
				return &entry
			}
		}
		return nil
	}

	current := byID("chatgpt")
	require.NotNil(t, current, "chatgpt must stay in the compiled-in defaults")
	require.Contains(t, current.Signatures.BundleIDs, "com.openai.codex",
		"the bundle id the current app moved to is the only thing that detects it")
	require.Contains(t, current.Signatures.ProcessNames, "ChatGPT",
		"without the process name an agent can only ever report the app as installed")

	classic := byID("chatgpt-classic")
	require.NotNil(t, classic, "chatgpt-classic must stay in the compiled-in defaults")
	require.Contains(t, classic.Signatures.BundleIDs, "com.openai.chat",
		"the bundle id Classic kept is what distinguishes it from the current app")
	require.Contains(t, classic.Signatures.ProcessNames, "ChatGPT Classic",
		"without the process name an agent can only ever report Classic as installed")

	// Process names are matched exactly, so the shorter name must not be a
	// prefix match that reports the current app on every Classic device.
	require.NotContains(t, classic.Signatures.ProcessNames, "ChatGPT")
	require.NotContains(t, current.Signatures.ProcessNames, "ChatGPT Classic")

	// Deliberately no config dirs. Classic inherited the original desktop
	// app's support directory and the current app keeps its own, and macOS
	// leaves both behind after an uninstall, so either would report someone
	// who no longer runs the app.
	require.Empty(t, classic.Signatures.ConfigDirs,
		"the inherited support directory would fire for people who no longer run Classic")
	require.Empty(t, current.Signatures.ConfigDirs,
		"a support directory outlives the app it belonged to")
}

// A decision about one product must not reach another. Both ChatGPT builds
// authorize through chatgpt.com and present the same documents, so the
// identity a caller proves is "a ChatGPT client" and goes no finer. The
// matchers therefore belong to the general app alone: registering them on
// Classic as well would make blocking Classic refuse everyone on the current
// app and on chatgpt.com.
func TestOnlyTheGeneralChatGPTTargetIsEnforceable(t *testing.T) {
	t.Parallel()

	var current, classic *aitargets.Target
	for _, entry := range aitargets.Defaults() {
		switch entry.ID {
		case "chatgpt":
			current = &entry
		case "chatgpt-classic":
			classic = &entry
		}
	}
	require.NotNil(t, current)
	require.NotNil(t, classic)

	require.True(t, aitargets.Enforceable(*current),
		"the ChatGPT documents have to live somewhere or no ChatGPT caller can be blocked")
	require.False(t, aitargets.Enforceable(*classic),
		"blocking Classic would refuse every ChatGPT caller, which no one asked for")
	require.True(t, classic.GatewayClient.IsZero(),
		"a self-reported client name would attribute shared traffic to Classic just as wrongly")
}

// builtinRow is the row BuiltInUpsertParams writes for a built-in: the
// organization's decision about it and nothing else.
func builtinRow(id string) repo.AiScanTarget {
	return repo.AiScanTarget{
		OrganizationID:  "org",
		ID:              id,
		DisplayName:     pgtype.Text{String: "", Valid: false},
		Category:        pgtype.Text{String: "", Valid: false},
		BundleIds:       []string{},
		Binaries:        []string{},
		ConfigDirs:      []string{},
		ProcessNames:    []string{},
		VersionPlistKey: pgtype.Text{String: "", Valid: false},
		CimdVendorKeys:  []string{},
		OauthClientIds:  []string{},
		ClientInfoNames: []string{},
		Status:          "unreviewed",
		Rationale:       pgtype.Text{String: "", Valid: false},
		CreatedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		UpdatedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
	}
}

// TestResolveBuiltinDefinitionRestoresWhatTheRowDoesNotStore guards a built-in
// toggle that worked once and then failed for good.
//
// Before the first toggle there is no row, so the pre-write read falls back to
// the compiled-in default: an omitted gateway_client is carried over and the
// read-only check passes. Afterwards the row exists, and reading it back gave
// an empty definition, so the very same toggle failed SameDefinition against
// the real built-in and was rejected as an edit.
func TestResolveBuiltinDefinitionRestoresWhatTheRowDoesNotStore(t *testing.T) {
	t.Parallel()

	var builtin aitargets.Target
	for _, candidate := range aitargets.Defaults() {
		if len(candidate.GatewayClient.OAuthClientIDs) > 0 {
			builtin = candidate
			break
		}
	}
	require.NotEmpty(t, builtin.ID, "expected a built-in carrying gateway matchers")

	fromRow := aitargets.EntryFromRow(builtinRow(builtin.ID)).Target
	require.Empty(t, fromRow.GatewayClient.OAuthClientIDs, "the row stores no definition; that is what makes resolution necessary")
	require.False(t, aitargets.SameDefinition(builtin, fromRow), "the unresolved row must not already look like the built-in")

	resolved := aitargets.ResolveBuiltinDefinition(fromRow)
	require.True(t, aitargets.SameDefinition(builtin, resolved), "a built-in read back from its row must still be the built-in")
	require.Equal(t, builtin.GatewayClient.OAuthClientIDs, resolved.GatewayClient.OAuthClientIDs)
	require.Equal(t, builtin.DisplayName, resolved.DisplayName)
}

// TestResolveBuiltinDefinitionLeavesOrganizationTargetsAlone: an organization's
// own target is stored in full, so there is nothing to restore and resolution
// must not overwrite it.
func TestResolveBuiltinDefinitionLeavesOrganizationTargetsAlone(t *testing.T) {
	t.Parallel()

	own := target("an-organization-target")
	require.Equal(t, own, aitargets.ResolveBuiltinDefinition(own))
}

// TestSameDefinitionFoldsTheDefaultVersionPlistKey: an omitted version hint
// resolves to DefaultVersionPlistKey, so sending that key explicitly says
// nothing an omission does not. A client that reads a built-in and sends the
// resolved default back was being told it had redefined a read-only target.
func TestSameDefinitionFoldsTheDefaultVersionPlistKey(t *testing.T) {
	t.Parallel()

	omitted := target("a-built-in")
	omitted.VersionHint = nil

	explicit := target("a-built-in")
	explicit.VersionHint = &aitargets.VersionHint{PlistKey: aitargets.DefaultVersionPlistKey}

	require.True(t, aitargets.SameDefinition(omitted, explicit),
		"the explicit default must compare equal to omitting the hint")

	// A genuinely different key is still a redefinition.
	other := target("a-built-in")
	other.VersionHint = &aitargets.VersionHint{PlistKey: "CFBundleVersion"}
	require.False(t, aitargets.SameDefinition(omitted, other),
		"a different key is a real change and must still be rejected")
}

// TestOverlayDropsTheDecisionRowOfARemovedBuiltIn: when a built-in leaves the
// registry, an organization that had decided about it keeps a row whose
// definition columns were always empty. Read back with no default to match,
// that row must not be promoted to an organization target, or agents would be
// served a nameless target and keep probing for a product that no longer
// ships.
func TestOverlayDropsTheDecisionRowOfARemovedBuiltIn(t *testing.T) {
	t.Parallel()

	orphan := aitargets.Entry{
		Target: aitargets.Target{
			ID:            "product-that-left-the-registry",
			DisplayName:   "",
			Category:      "",
			Signatures:    aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}},
			VersionHint:   nil,
			GatewayClient: aitargets.GatewayClient{CIMDVendorKeys: nil, OAuthClientIDs: nil, ClientInfoNames: nil},
		},
		Source:     aitargets.SourceOrganization,
		Customized: false,
		Decision:   aitargets.DecisionRecord{TargetID: "product-that-left-the-registry", Decision: aitargets.DecisionBlocked, Rationale: "not approved"},
		CreatedAt:  time.Time{},
		UpdatedAt:  time.Time{},
	}
	own := aitargets.Entry{
		Target:     target("an-organization-target"),
		Source:     aitargets.SourceOrganization,
		Customized: false,
		Decision:   aitargets.UnreviewedDecisionRecord("an-organization-target"),
		CreatedAt:  time.Time{},
		UpdatedAt:  time.Time{},
	}

	entries := aitargets.Overlay(aitargets.Defaults(), []aitargets.Entry{orphan, own})

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	require.NotContains(t, ids, orphan.ID, "a decision-only row with no default behind it must not be served")
	require.Contains(t, ids, own.ID, "an organization's own target carries its definition and stays")
}
