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

// TestCatalogAdmits_GitHubCopilotCLIExactly pins the narrow rule for a client
// hosted on github.com, where admitting a path namespace would also trust
// unrelated user-controlled content on the same host.
func TestCatalogAdmits_GitHubCopilotCLIExactly(t *testing.T) {
	t.Parallel()

	const clientID = "https://github.com/copilot/cli/client-metadata.json"

	reason, ok := CatalogMatch(clientID)
	require.True(t, ok)
	require.Equal(t, AdmitCatalogExact, reason)

	nearMisses := []string{
		clientID + "/",
		"https://github.com/copilot/client-metadata.json",
		"https://github.com/copilot/zzz-not-real/client-metadata.json",
		"https://github.com/copilot/cli/zzz.json",
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
	first[0] = Preset{VendorKey: "attacker", DisplayName: "attacker", URL: unknownURL, Enabled: true}

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

// TestCatalogMatch_OverlappedLiteralReportsAsExact: entries may overlap, and
// a client_id is admitted when at least one enabled entry matches. The stable
// Codex document sits inside OpenAI's connector wildcard, so both admit it.
// Exact entries are consulted first, so it reports as exact and the
// wildcard's share counts only the traffic it alone carries.
func TestCatalogMatch_OverlappedLiteralReportsAsExact(t *testing.T) {
	t.Parallel()

	reason, ok := CatalogMatch("https://chatgpt.com/oauth/codex/client.json")
	require.True(t, ok)
	require.Equal(t, AdmitCatalogExact, reason)

	reason, ok = CatalogMatch(chatGPTConnectorURL)
	require.True(t, ok)
	require.Equal(t, AdmitCatalogPattern, reason, "an id only the wildcard covers still reports as a pattern")
}

// TestCatalogPreset_OverlappedLiteralBeatsTheWildcard pins how an overlap is
// attributed.
//
// OpenAI's connector wildcard admits the stable Codex document, because
// "codex" occupies its single wildcard segment. Both entries therefore match,
// and only the literal names the product. Resolving the wildcard instead
// would hand a Codex caller ChatGPT's identity, and the MCP gateway would
// judge it under whatever access decision the organization made about
// ChatGPT.
func TestCatalogPreset_OverlappedLiteralBeatsTheWildcard(t *testing.T) {
	t.Parallel()

	const stableDocument = "https://chatgpt.com/oauth/codex/client.json"

	preset, ok := CatalogPreset(stableDocument)
	require.True(t, ok)
	require.Equal(t, stableDocument, preset.URL, "the literal entry must win over the wildcard that also admits it")
	require.Equal(t, "Codex CLI (stable document)", preset.DisplayName)

	connector, ok := CatalogPreset(chatGPTConnectorURL)
	require.True(t, ok)
	require.Equal(t, chatGPTPattern, connector.URL, "an id only the wildcard covers still resolves to the wildcard")
}

// TestCatalog_DisabledEntriesAreNotAdmitted is the guard for the failure
// cubic caught: setting Enabled=false on a row whose URL falls inside a
// wildcard leaves it reported as disabled by the management API while
// /authorize still accepts it.
//
// Admission is an OR, so disabling one of two overlapping entries cannot
// de-admit the URL by itself. This test does not paper over that the way a
// per-row "not really a rule" flag did; it fails loudly, so whoever disabled
// the row finds out the covering wildcard has to be narrowed too.
//
// Vacuous while every entry is enabled, which is the point — it fails the
// build the moment someone disables one that another rule still covers.
func TestCatalog_DisabledEntriesAreNotAdmitted(t *testing.T) {
	t.Parallel()

	for _, preset := range Catalog() {
		if preset.Enabled {
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

// TestCatalogPreset_NamesTheVendorBehindAClientID: policy is written about a
// product, not about the id a client happened to present. A vendor minting
// one document per MCP server has no literal id to write down, so the
// pattern entry is the only stable handle on it.
func TestCatalogPreset_NamesTheVendorBehindAClientID(t *testing.T) {
	t.Parallel()

	preset, ok := CatalogPreset(claudeCodeURL)
	require.True(t, ok)
	require.Equal(t, "anthropic", preset.VendorKey)
	require.Equal(t, claudeCodeURL, preset.URL)

	preset, ok = CatalogPreset(chatGPTConnectorURL)
	require.True(t, ok)
	require.Equal(t, "openai", preset.VendorKey)
	require.True(t, preset.IsPattern(), "a per-connector id resolves to the wildcard that admits it")

	_, ok = CatalogPreset(unknownURL)
	require.False(t, ok)
}

// TestCatalogPreset_AgreesWithCatalogMatch pins the two lookups to one
// answer. A client this catalog admits must always be nameable, or a policy
// written about a vendor would silently miss traffic admission let through.
func TestCatalogPreset_AgreesWithCatalogMatch(t *testing.T) {
	t.Parallel()

	for _, preset := range Catalog() {
		if !preset.Enabled || preset.IsPattern() {
			continue
		}
		_, admitted := CatalogMatch(preset.URL)
		resolved, named := CatalogPreset(preset.URL)
		require.Equal(t, admitted, named, "catalog entry %q", preset.URL)
		if named {
			require.NotEmpty(t, resolved.VendorKey, "catalog entry %q", preset.URL)
		}
	}
}
