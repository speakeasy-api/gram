package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The canonical form collapses exactly three things: scheme case, host case,
// the scheme's default port, and trailing slashes. Each case below names which.
func TestParseCanonicalIssuerURL_Canonicalizes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "already canonical", in: "https://idp.example.com", want: "https://idp.example.com"},
		{name: "trailing slash", in: "https://idp.example.com/", want: "https://idp.example.com"},
		{name: "repeated trailing slashes", in: "https://idp.example.com///", want: "https://idp.example.com"},
		{name: "trailing slash after path", in: "https://idp.example.com/oauth/", want: "https://idp.example.com/oauth"},
		{name: "surrounding whitespace", in: "  https://idp.example.com  ", want: "https://idp.example.com"},
		{name: "uppercase scheme", in: "HTTPS://idp.example.com", want: "https://idp.example.com"},
		{name: "uppercase host", in: "https://IDP.Example.COM", want: "https://idp.example.com"},
		{name: "https default port", in: "https://idp.example.com:443/oauth", want: "https://idp.example.com/oauth"},
		{name: "http default port", in: "http://idp.example.com:80/oauth", want: "http://idp.example.com/oauth"},
		{name: "everything at once", in: " HTTPS://IDP.Example.com:443/OAuth/ ", want: "https://idp.example.com/OAuth"},
		{name: "ipv6 literal stays bracketed", in: "https://[2001:DB8::1]:443/oauth", want: "https://[2001:db8::1]/oauth"},
		// The query and fragment rejections test the raw string for a literal "?"
		// or "#". An encoded delimiter is part of the path, not a delimiter, and
		// must survive rather than trip those checks.
		{name: "percent-encoded delimiters stay in the path", in: "https://idp.example.com/p%23q%3Fr", want: "https://idp.example.com/p%23q%3Fr"},
	}

	for _, tc := range cases {
		canonical, err := parseCanonicalIssuerURL(tc.in)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, canonical.String(), tc.name)
	}
}

// Everything here is deliberately NOT collapsed. Over-matching would attach a
// tenant to somebody else's upstream, so each of these stays a distinct issuer
// even though a more aggressive normalizer would equate them.
func TestParseCanonicalIssuerURL_PreservesDistinctions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    string
		b    string
	}{
		{name: "http is not https", a: "http://idp.example.com", b: "https://idp.example.com"},
		{name: "path case is significant", a: "https://idp.example.com/OAuth", b: "https://idp.example.com/oauth"},
		{name: "non-default port is significant", a: "https://idp.example.com:8443", b: "https://idp.example.com"},
		{name: "http default port is not the https one", a: "http://idp.example.com:443", b: "http://idp.example.com"},
		{name: "percent-encoding is not decoded", a: "https://idp.example.com/%7Euser", b: "https://idp.example.com/~user"},
		{name: "inner slashes are not collapsed", a: "https://idp.example.com/a//b", b: "https://idp.example.com/a/b"},
		{name: "dot segments are not resolved", a: "https://idp.example.com/a/../b", b: "https://idp.example.com/b"},
		{name: "punycode is not equated to unicode", a: "https://xn--bcher-kva.example.com", b: "https://bücher.example.com"},
		{name: "subdomain is not the apex", a: "https://idp.example.com", b: "https://example.com"},
	}

	for _, tc := range cases {
		a, err := parseCanonicalIssuerURL(tc.a)
		require.NoError(t, err, tc.name)
		b, err := parseCanonicalIssuerURL(tc.b)
		require.NoError(t, err, tc.name)
		require.NotEqual(t, a.String(), b.String(), tc.name)
	}
}

func TestParseCanonicalIssuerURL_Rejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "whitespace only", in: "   "},
		{name: "relative", in: "idp.example.com/oauth"},
		{name: "scheme relative", in: "//idp.example.com"},
		{name: "no host", in: "https:///oauth"},
		{name: "not http", in: "ftp://idp.example.com"},
		{name: "javascript scheme", in: "javascript:alert(1)"},
		{name: "mailto", in: "mailto:someone@example.com"},
		{name: "userinfo", in: "https://user:pass@idp.example.com"},
		// RFC 8414 §2 forbids both on an issuer identifier, so a URL carrying one
		// is not an issuer and must not become a lookup key. The empty forms are
		// the interesting ones: net/url reports no fragment at all for a bare "#",
		// so a check on the parsed fields alone would let it through and quietly
		// canonicalize it to the fragment-free URL.
		{name: "query string", in: "https://idp.example.com?tenant=acme"},
		{name: "empty query string", in: "https://idp.example.com?"},
		{name: "fragment", in: "https://idp.example.com#section"},
		{name: "empty fragment", in: "https://idp.example.com#"},
		{name: "empty fragment after path", in: "https://idp.example.com/oauth#"},
		{name: "fragment and query", in: "https://idp.example.com?a=b#c"},
	}

	for _, tc := range cases {
		_, err := parseCanonicalIssuerURL(tc.in)
		require.Error(t, err, tc.name)
	}
}

// The candidate set is what the lookup query probes against the raw issuer
// column. It must stay closed: these assertions pin both its contents and its
// size so a future change that starts enumerating host-case or encoding
// variants fails here rather than silently making the lookup unbounded.
func TestCanonicalIssuerURL_MatchCandidates(t *testing.T) {
	t.Parallel()

	canonical, err := parseCanonicalIssuerURL("https://idp.example.com/oauth")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"https://idp.example.com/oauth",
		"https://idp.example.com/oauth/",
		"https://idp.example.com:443/oauth",
		"https://idp.example.com:443/oauth/",
	}, canonical.matchCandidates())
}

// A non-default port has no default-port spelling, so the set shrinks rather
// than emitting a nonsensical https://host:8443:443 entry.
func TestCanonicalIssuerURL_MatchCandidatesExplicitPort(t *testing.T) {
	t.Parallel()

	canonical, err := parseCanonicalIssuerURL("https://idp.example.com:8443/oauth")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"https://idp.example.com:8443/oauth",
		"https://idp.example.com:8443/oauth/",
	}, canonical.matchCandidates())
}

// The caller's own spelling is probed alongside the canonical ones. This is what
// lets a second install that discovers the same oddly-spelled URL as the first
// still find that row, even though canonicalization never touches stored values.
func TestCanonicalIssuerURL_MatchCandidatesIncludesRawSpelling(t *testing.T) {
	t.Parallel()

	canonical, err := parseCanonicalIssuerURL("https://IDP.Example.com/oauth")
	require.NoError(t, err)
	require.Contains(t, canonical.matchCandidates(), "https://IDP.Example.com/oauth")
	require.Contains(t, canonical.matchCandidates(), "https://idp.example.com/oauth")
}

// A raw spelling that is already canonical must not be emitted twice, or the
// lookup would probe the same value repeatedly.
func TestCanonicalIssuerURL_MatchCandidatesDeduplicates(t *testing.T) {
	t.Parallel()

	canonical, err := parseCanonicalIssuerURL("https://idp.example.com/oauth")
	require.NoError(t, err)

	candidates := canonical.matchCandidates()
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		_, duplicate := seen[candidate]
		require.False(t, duplicate, "duplicate candidate %q", candidate)
		seen[candidate] = struct{}{}
	}
}
