package admission

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogAdmits_EnabledEntries(t *testing.T) {
	t.Parallel()

	for _, preset := range Catalog() {
		// Patterns are skipped: matching one against its own literal text
		// succeeds trivially and asserts nothing. Their real coverage is in
		// pattern_test.go, against the client_ids they are meant to admit.
		if preset.Enabled && !preset.IsPattern() {
			require.Truef(t, catalogAdmits(preset.URL), "enabled preset %q must be admitted", preset.URL)
		}
	}
}

func TestCatalogAdmits_RejectsUnknown(t *testing.T) {
	t.Parallel()

	require.False(t, catalogAdmits(unknownURL))
	require.False(t, catalogAdmits(""))
}

// TestCatalogAdmits_IsExactMatch pins the matching rule. Draft-02 §3 forbids
// normalization, so near-misses on the same origin must NOT be admitted —
// an origin-level allowlist would be far broader than intended.
func TestCatalogAdmits_IsExactMatch(t *testing.T) {
	t.Parallel()

	nearMisses := []string{
		"https://claude.ai/oauth/claude-code-client-metadata/",                 // trailing slash
		"https://claude.ai:443/oauth/claude-code-client-metadata",              // explicit default port
		"https://CLAUDE.AI/oauth/claude-code-client-metadata",                  // host case
		"https://claude.ai/oauth/claude-code-client-metadata?x=1",              // query
		"https://claude.ai/oauth/attacker-controlled-client-metadata",          // same origin, other path
		"https://claude.ai.evil.example.com/oauth/claude-code-client-metadata", // suffix-extended host
	}
	for _, url := range nearMisses {
		require.Falsef(t, catalogAdmits(url), "near-miss %q must not be admitted", url)
	}
}

// TestCatalog_EntriesAreWellFormed guards the constant itself: every entry
// must be a syntactically valid CIMD client_id, or it is dead policy that
// can never match a real presentation.
func TestCatalog_EntriesAreWellFormed(t *testing.T) {
	t.Parallel()

	seen := map[string]struct{}{}
	for _, preset := range Catalog() {
		require.NotEmpty(t, preset.VendorKey, "vendor key is required")
		require.NotEmpty(t, preset.DisplayName, "display name is required")
		require.True(t, strings.HasPrefix(preset.URL, "https://"), "preset %q must be https", preset.URL)
		require.NotContains(t, preset.URL, "#", "preset %q must not carry a fragment", preset.URL)
		require.LessOrEqual(t, len(preset.URL), MaxClientIDLength, "preset %q exceeds the client_id cap", preset.URL)

		_, duplicate := seen[preset.URL]
		require.Falsef(t, duplicate, "duplicate catalog URL %q", preset.URL)
		seen[preset.URL] = struct{}{}
	}
}

// TestCatalog_ReturnsCopy: the catalog is process-global state, so a caller
// mutating the returned slice must not be able to change admission for
// every issuer in the process.
func TestCatalog_ReturnsCopy(t *testing.T) {
	t.Parallel()

	first := Catalog()
	require.NotEmpty(t, first)
	original := first[0]
	first[0] = Preset{VendorKey: "attacker", DisplayName: "attacker", URL: unknownURL, DisplayOnly: false, Enabled: true}

	second := Catalog()
	require.Equal(t, original, second[0])
	require.False(t, catalogAdmits(unknownURL))
}

// TestCatalogMatch_ReportsWhichEntryMatched: the exact/pattern split is the
// only telemetry that can show whether a wildcard entry is doing any work,
// so getting the reason wrong would silently misattribute traffic.
func TestCatalogMatch_ReportsWhichEntryMatched(t *testing.T) {
	t.Parallel()

	reason, ok := CatalogMatch(claudeCodeURL)
	require.True(t, ok)
	require.Equal(t, AdmitCatalogExact, reason)

	reason, ok = CatalogMatch(chatGPTConnectorURL)
	require.True(t, ok)
	require.Equal(t, AdmitCatalogPattern, reason)

	reason, ok = CatalogMatch(unknownURL)
	require.False(t, ok)
	require.Empty(t, reason, "a miss must carry no reason")
}

// TestCatalogMatch_DisplayOnlyEntriesAttributeToTheirRule: a DisplayOnly
// row names a URL that some other entry admits, so the admission must be
// attributed to the rule that actually made it. Codex CLI is exactly this
// case — listed by name, admitted by the ChatGPT connector wildcard.
//
// Reporting it as an exact match would misattribute the traffic and hide
// how much work the wildcard is doing, which is the one thing the
// exact/pattern split exists to measure.
func TestCatalogMatch_DisplayOnlyEntriesAttributeToTheirRule(t *testing.T) {
	t.Parallel()

	reason, ok := CatalogMatch("https://chatgpt.com/oauth/codex/client.json")
	require.True(t, ok)
	require.Equal(t, AdmitCatalogPattern, reason)
}

// TestCatalog_DisplayOnlyEntriesAreCoveredByARule pins the invariant that
// makes DisplayOnly safe: a row excluded from matching must still be
// admitted by something, or marking it DisplayOnly silently de-admitted a
// client the catalog claims to support.
func TestCatalog_DisplayOnlyEntriesAreCoveredByARule(t *testing.T) {
	t.Parallel()

	var found int
	for _, preset := range Catalog() {
		if !preset.DisplayOnly {
			continue
		}
		found++
		require.Falsef(t, preset.IsPattern(), "a DisplayOnly entry names one URL, so %q must not be a pattern", preset.URL)
		require.Truef(t, catalogAdmits(preset.URL), "DisplayOnly entry %q is admitted by nothing", preset.URL)
	}
	require.Positive(t, found, "expected at least one DisplayOnly entry; delete this guard deliberately if they were removed")
}

// TestCatalog_DisabledEntriesAreNotAdmitted is the guard for the failure
// cubic caught: before DisplayOnly existed, setting Enabled=false on a row
// whose URL fell inside a wildcard left it reported as disabled by the
// management API while /authorize still accepted it.
//
// Vacuous while every entry is enabled, which is the point — it fails the
// build the moment someone disables one that another rule still covers.
func TestCatalog_DisabledEntriesAreNotAdmitted(t *testing.T) {
	t.Parallel()

	for _, preset := range Catalog() {
		if preset.Enabled || preset.DisplayOnly {
			continue
		}
		require.Falsef(t, catalogAdmits(preset.URL),
			"%q is disabled but another catalog entry still admits it", preset.URL)
	}
}

// catalogAdmits is the bool-only view of CatalogMatch, for the many
// assertions that care whether a URL is admitted and not by which entry.
func catalogAdmits(clientID string) bool {
	_, ok := CatalogMatch(clientID)
	return ok
}
