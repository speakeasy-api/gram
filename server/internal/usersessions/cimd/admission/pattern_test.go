package admission

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	chatGPTPattern = "https://chatgpt.com/oauth/*/client.json"
	codexPattern   = "https://chatgpt.com/oauth/codex/*/client.json"
)

func TestPresetIsPattern(t *testing.T) {
	t.Parallel()

	require.True(t, Preset{URL: chatGPTPattern}.IsPattern())
	require.False(t, Preset{URL: "https://chatgpt.com/oauth/abc/client.json"}.IsPattern())
	require.False(t, Preset{URL: ""}.IsPattern())
}

func TestMatchesPattern_AdmitsOneSegment(t *testing.T) {
	t.Parallel()

	admitted := []string{
		"https://chatgpt.com/oauth/abc123/client.json",
		"https://chatgpt.com/oauth/codex/client.json",
		"https://chatgpt.com/oauth/A-Z_0-9.~/client.json",
	}
	for _, clientID := range admitted {
		require.Truef(t, matchesPattern(chatGPTPattern, clientID), "%q should match", clientID)
	}
}

// TestMatchesPattern_HostEscapesRejected is the test that matters most.
// Every entry here is a host the pattern must NOT reach; a wildcard that
// widens the authority is the classic redirect-URI wildcard vulnerability,
// and it is the reason wildcards are confined to the reviewed catalog.
func TestMatchesPattern_HostEscapesRejected(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"https://chatgpt.com.evil.example.com/oauth/x/client.json", // suffix-extended host
		"https://evil.example.com/oauth/x/client.json",             // unrelated host
		"https://chatgpt.com@evil.example.com/oauth/x/client.json", // userinfo confusion
		"https://evil.example.com/chatgpt.com/oauth/x/client.json", // host in the path
		"https://chatgpt.com:8443/oauth/x/client.json",             // explicit port
		"https://CHATGPT.COM/oauth/x/client.json",                  // host case
		"http://chatgpt.com/oauth/x/client.json",                   // scheme downgrade
		"https://chatgpt.com./oauth/x/client.json",                 // trailing-dot host
	}
	for _, clientID := range rejected {
		require.Falsef(t, matchesPattern(chatGPTPattern, clientID), "%q must NOT match", clientID)
	}
}

// TestMatchesPattern_DoesNotSpanPathSeparator: a wildcard stands for one
// complete segment. Spanning "/" would let a pattern reach arbitrarily deep
// into a vendor's URL space.
func TestMatchesPattern_DoesNotSpanPathSeparator(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"https://chatgpt.com/oauth/a/b/client.json",       // two segments for one *
		"https://chatgpt.com/oauth/client.json",           // zero segments
		"https://chatgpt.com/oauth//client.json",          // empty segment
		"https://chatgpt.com/oauth/a/b/c/d/client.json",   // deeper still
		"https://chatgpt.com/oauth/x/client.json/extra",   // trailing segment
		"https://chatgpt.com/other/x/client.json",         // literal segment differs
		"https://chatgpt.com/oauth/x/client.json?probe=1", // query is part of the compare
	}
	for _, clientID := range rejected {
		require.Falsef(t, matchesPattern(chatGPTPattern, clientID), "%q must NOT match", clientID)
	}
}

// TestMatchesPattern_QueryCannotSmuggleSegments is a regression test.
//
// validatePattern cut the pattern at "?"/"#" before segmenting, but
// matchesPattern segmented the raw remainder of both sides. A "/" inside a
// presented query therefore acted as a path separator, letting a client_id
// match with one fewer real path segment than the pattern names: for
// /oauth/*/client.json, a client_id of /oauth/authorize?x=/client.json split
// into ["oauth", "authorize?x=", "client.json"] and matched, even though its
// actual path is /oauth/authorize.
//
// The host stayed literal throughout, so this was never a cross-origin
// escape — but the wildcard reached resources the catalog never named.
func TestMatchesPattern_QueryCannotSmuggleSegments(t *testing.T) {
	t.Parallel()

	smuggled := []string{
		"https://chatgpt.com/oauth/authorize?x=/client.json", // real path /oauth/authorize
		"https://chatgpt.com/oauth/?a=/client.json",          // real path /oauth/
		"https://chatgpt.com/oauth/authorize#/client.json",   // fragment variant
		"https://chatgpt.com/oauth?a=/x/client.json",         // one segment shallower still
	}
	for _, clientID := range smuggled {
		require.Falsef(t, matchesPattern(chatGPTPattern, clientID), "%q must NOT match", clientID)
		require.Falsef(t, catalogAdmits(clientID), "%q must NOT be admitted by the catalog", clientID)
	}
}

// TestMatchesPattern_SuffixMustMatchLiterally: a query or fragment is
// compared byte for byte, so a pattern with no suffix admits only client_ids
// with no suffix.
func TestMatchesPattern_SuffixMustMatchLiterally(t *testing.T) {
	t.Parallel()

	require.True(t, matchesPattern(chatGPTPattern, "https://chatgpt.com/oauth/abc/client.json"))
	require.False(t, matchesPattern(chatGPTPattern, "https://chatgpt.com/oauth/abc/client.json?v=2"))
	require.False(t, matchesPattern(chatGPTPattern, "https://chatgpt.com/oauth/abc/client.json#f"))

	// And a pattern that does carry a literal suffix requires it.
	withQuery := "https://example.com/oauth/*/client.json?v=2"
	require.NoError(t, validatePattern(withQuery))
	require.True(t, matchesPattern(withQuery, "https://example.com/oauth/abc/client.json?v=2"))
	require.False(t, matchesPattern(withQuery, "https://example.com/oauth/abc/client.json"))
}

// TestMatchesPattern_EncodedSeparatorRejected: matching operates on raw
// strings with no percent-decoding, so %2F cannot smuggle a "/" into what
// must be a single segment. Decoding first would make this match.
func TestMatchesPattern_EncodedSeparatorRejected(t *testing.T) {
	t.Parallel()

	// Raw %2F stays inside one segment, so this DOES match the pattern —
	// and that is correct: it is a single segment whose name contains an
	// escaped slash, and it addresses a different resource than a real "/".
	require.True(t, matchesPattern(chatGPTPattern, "https://chatgpt.com/oauth/a%2Fb/client.json"))
	// What must not happen is the reverse: a real separator being folded
	// into one segment.
	require.False(t, matchesPattern(chatGPTPattern, "https://chatgpt.com/oauth/a/b/client.json"))
}

func TestValidatePattern_AcceptsSegmentWildcard(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://chatgpt.com/oauth/*/client.json",
		"https://example.com/*",
		"https://example.com/a/*/b/*/c",
	}
	for _, pattern := range valid {
		require.NoErrorf(t, validatePattern(pattern), "%q should be a valid pattern", pattern)
	}
}

// TestValidatePattern_RejectsHostWildcard prevents the dangerous patterns
// from ever reaching the catalog in the first place. Matching is the second
// line of defence; this is the first.
func TestValidatePattern_RejectsHostWildcard(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"https://*/oauth/client.json",   // any host
		"https://*.chatgpt.com/x",       // subdomain wildcard
		"https://chatgpt.com*/x",        // suffix-extended host
		"https://chatgpt*.com/x",        // infix host wildcard
		"http*://chatgpt.com/x",         // scheme wildcard
		"https://user*@chatgpt.com/x",   // userinfo wildcard
		"https://chatgpt.com/x?probe=*", // query wildcard
		"https://chatgpt.com/x#*",       // fragment wildcard
	}
	for _, pattern := range invalid {
		require.Errorf(t, validatePattern(pattern), "%q must be rejected", pattern)
	}
}

func TestValidatePattern_RejectsPartialSegment(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"https://chatgpt.com/oauth/cli*ent.json",
		"https://chatgpt.com/oauth/*.json",
		"https://chatgpt.com/oauth/prefix*/client.json",
	}
	for _, pattern := range invalid {
		require.ErrorIsf(t, validatePattern(pattern), errPatternPartialSegment, "%q must be rejected", pattern)
	}
}

func TestValidatePattern_RejectsMalformed(t *testing.T) {
	t.Parallel()

	// No "/" at all: rejected before the path check, since there is no
	// authority/path boundary to split on.
	require.ErrorIs(t, validatePattern("https://chatgpt.com"), errPatternUnparseable)
	// Authority present, path empty.
	require.ErrorIs(t, validatePattern("https://chatgpt.com/"), errPatternNoPathComponent)
	require.ErrorIs(t, validatePattern("https://chatgpt.com/a//b"), errPatternEmptySegment)
	require.ErrorIs(t, validatePattern("ftp://chatgpt.com/x"), errPatternUnparseable)
	require.ErrorIs(t, validatePattern("https:///x"), errPatternUnparseable)
}

// TestCatalog_PatternsAreValid is the guard that makes the whole design
// safe: it runs validatePattern over every wildcard actually shipped, so a
// dangerous catalog entry fails the build rather than silently widening
// admission in production.
func TestCatalog_PatternsAreValid(t *testing.T) {
	t.Parallel()

	var found int
	for _, preset := range Catalog() {
		if !preset.IsPattern() {
			continue
		}
		found++
		require.NoErrorf(t, validatePattern(preset.URL), "catalog pattern %q is unsafe", preset.URL)
	}
	require.Positive(t, found, "expected at least one catalog pattern; if patterns were removed, delete this guard deliberately")
}

// TestCatalogAdmits_ChatGPTConnectorNamespace exercises the end-to-end
// reason wildcards exist: an unbounded, server-generated connector id.
func TestCatalogAdmits_ChatGPTConnectorNamespace(t *testing.T) {
	t.Parallel()

	require.True(t, catalogAdmits("https://chatgpt.com/oauth/client.json"))
	require.True(t, catalogAdmits("https://chatgpt.com/oauth/codex/client.json"))
	require.True(t, catalogAdmits("https://chatgpt.com/oauth/"+strings.Repeat("a", 40)+"/client.json"))

	require.False(t, catalogAdmits("https://chatgpt.com.evil.example.com/oauth/x/client.json"))
	require.False(t, catalogAdmits("https://chatgpt.com/oauth/x/y/client.json"))
}

// TestCatalogAdmits_CodexPerServerNamespace exercises the Codex CLI pattern
// against the client_id shape Codex ≥0.148.0 presents: {id} is a 12-char
// base64url-no-pad digest of the MCP server URL, so real ids draw only on
// A-Z, a-z, 0-9, "-", and "_" — an alphabet containing no "/", which is
// what makes single-segment matching sound for this namespace.
func TestCatalogAdmits_CodexPerServerNamespace(t *testing.T) {
	t.Parallel()

	// The first two are real ids observed in production denials (AIS-582);
	// between them they cover the "-" alphabet character, and the third
	// covers "_".
	admitted := []string{
		"https://chatgpt.com/oauth/codex/WoV6218LroET/client.json",
		"https://chatgpt.com/oauth/codex/Lv2-Pvw8pHkH/client.json",
		"https://chatgpt.com/oauth/codex/p9jr1GJD7_bK/client.json",
	}
	for _, clientID := range admitted {
		require.Truef(t, matchesPattern(codexPattern, clientID), "%q should match", clientID)
		require.Truef(t, catalogAdmits(clientID), "%q should be admitted by the catalog", clientID)
	}

	rejected := []string{
		"https://chatgpt.com/oauth/codex/client.json",                    // zero segments for the *
		"https://chatgpt.com/oauth/codex/a/b/client.json",                // two segments for one *
		"https://chatgpt.com/oauth/codex//client.json",                   // empty segment
		"https://chatgpt.com/oauth/other/abc123/client.json",             // literal segment differs
		"https://chatgpt.com/oauth/codex/x/client.json?probe=1",          // query is part of the compare
		"https://chatgpt.com.evil.example.com/oauth/codex/x/client.json", // suffix-extended host
	}
	for _, clientID := range rejected {
		require.Falsef(t, matchesPattern(codexPattern, clientID), "%q must NOT match", clientID)
	}
	// The deeper shape must miss the whole catalog, not just this pattern.
	require.False(t, catalogAdmits("https://chatgpt.com/oauth/codex/a/b/client.json"))
}

// TestCatalog_CodexStableDocumentAttribution pins which rule covers the
// DisplayOnly stable Codex document: the one-segment connector wildcard,
// not the Codex per-server pattern, which requires a segment between
// "codex" and "client.json". A catalog edit that shifts this coverage
// should fail here rather than move the invariant silently.
func TestCatalog_CodexStableDocumentAttribution(t *testing.T) {
	t.Parallel()

	const stableDocument = "https://chatgpt.com/oauth/codex/client.json"
	require.True(t, matchesPattern(chatGPTPattern, stableDocument))
	require.False(t, matchesPattern(codexPattern, stableDocument))
}

// TestCatalogAdmits_ExactEntriesUnaffectedByPatterns: adding wildcards must
// not have loosened matching for ordinary entries.
func TestCatalogAdmits_ExactEntriesUnaffectedByPatterns(t *testing.T) {
	t.Parallel()

	require.True(t, catalogAdmits(claudeCodeURL))
	require.False(t, catalogAdmits("https://claude.ai/oauth/anything-else"))
	require.False(t, catalogAdmits("https://claude.ai/oauth/claude-code-client-metadata/x"))
}
