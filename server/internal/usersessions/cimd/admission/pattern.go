package admission

import (
	"errors"
	"net/url"
	"strings"
)

// Wildcard matching exists for exactly one shape: vendors that mint a
// distinct Client ID Metadata Document per connector, per install, or per
// tenant, so their client_id is drawn from an unbounded namespace no
// allowlist can enumerate. OpenAI is the motivating case — every ChatGPT
// connector gets its own https://chatgpt.com/oauth/{id}/client.json, where
// {id} is server-generated.
//
// It is deliberately available ONLY to the compile-time catalog, never to
// operator-supplied custom URLs. A pattern is a security-relevant grant of
// trust to a whole namespace, and confining it to code keeps every one of
// them behind review and deploy. Customers who need a specific URL add that
// URL exactly, which is what they want anyway.
//
// The syntax is one metacharacter, `*`, with two hard structural limits:
//
//   - The scheme and host must be entirely literal. A wildcard that could
//     widen the host is the classic redirect-URI wildcard vulnerability:
//     "https://claude.ai*" matches https://claude.ai.evil.example.com, and
//     "https://*/x" matches anything at all. Host-literal also keeps Gram's
//     same-origin redirect_uri binding meaningful, since the origin a
//     pattern admits is fixed.
//
//   - A `*` stands for exactly one COMPLETE path segment and never matches
//     across "/". So /oauth/*/client.json admits /oauth/abc/client.json but
//     not /oauth/a/b/client.json, and /oauth/cli*ent.json is not a legal
//     pattern at all. Segment-scoping stops a pattern from reaching deeper
//     into a vendor's URL space than intended.
//
// Consequence worth stating: `*` is a legal character in a real URL path
// (RFC 3986 sub-delims), so a literal `*` in a document URL cannot be
// expressed in the catalog. No known vendor publishes such a URL, and an
// escape syntax would cost more than it buys.

// errPatternHostWildcard and friends explain why a catalog pattern is
// malformed. They surface only through the catalog's own validation test —
// patterns are compile-time constants, so a bad one is a build-time bug,
// never a runtime condition.
var (
	errPatternHostWildcard    = errors.New("wildcard is not permitted in the scheme or host")
	errPatternPartialSegment  = errors.New("wildcard must be a complete path segment")
	errPatternUnparseable     = errors.New("pattern is not a parseable https URL")
	errPatternEmptySegment    = errors.New("pattern has an empty path segment")
	errPatternNoPathComponent = errors.New("pattern has no path component")
)

// isPattern reports whether a catalog URL carries a wildcard. Exact entries
// take a map lookup; only patterns pay for matching. Callers outside the
// package ask [Preset.IsPattern] instead, since a bare URL string is never
// the thing they hold.
func isPattern(url string) bool {
	return strings.Contains(url, "*")
}

// validatePattern enforces the structural rules above. Returns nil for a
// pattern that is safe to match against.
//
// It deliberately validates the RAW string using the same split that
// matchesPattern applies, rather than inspecting a parsed *url.URL. Going
// through url.Parse re-encodes as it goes — url.Userinfo.String() escapes
// "*" to "%2A", so "https://user*@host/x" appears wildcard-free on the
// parsed value while still being a userinfo wildcard in the string that
// actually gets compared. Validating exactly what matching consumes
// removes that entire class of mismatch.
func validatePattern(pattern string) error {
	prefix, rest, ok := splitAuthorityAndPath(pattern)
	if !ok {
		return errPatternUnparseable
	}
	// The scheme + authority (including any userinfo and port) is compared
	// byte for byte at match time, so it must carry no wildcard.
	if strings.Contains(prefix, "*") {
		return errPatternHostWildcard
	}
	// url.Parse still earns its keep as a structural sanity check on the
	// whole pattern, with wildcards stubbed out so it sees a normal URL.
	if _, err := url.Parse(strings.ReplaceAll(pattern, "*", "x")); err != nil {
		return errPatternUnparseable
	}
	if rest == "" {
		return errPatternNoPathComponent
	}
	// A wildcard in the query or fragment is rejected: matchesPattern
	// compares the suffix literally, so one there could never match
	// anything. Uses the same cut matchesPattern applies, which is what
	// keeps validation and matching from diverging.
	path, suffix := splitPathAndSuffix(rest)
	if strings.Contains(suffix, "*") {
		return errPatternHostWildcard
	}

	// Path segments: a wildcard segment must be exactly "*".
	for segment := range strings.SplitSeq(path, "/") {
		if segment == "" {
			return errPatternEmptySegment
		}
		if strings.Contains(segment, "*") && segment != "*" {
			return errPatternPartialSegment
		}
	}
	return nil
}

// matchesPattern reports whether a presented client_id matches a catalog
// pattern. The pattern must already have passed validatePattern; the
// catalog's construction guarantees that.
//
// Matching operates on the RAW strings, with no normalization and no
// percent-decoding, consistent with -02 §3's simple-string-comparison rule
// and with how exact entries are compared. Decoding first would let
// %2F smuggle a "/" into what must be a single segment.
func matchesPattern(pattern, clientID string) bool {
	patternPrefix, patternRest, ok := splitAuthorityAndPath(pattern)
	if !ok {
		return false
	}
	clientPrefix, clientRest, ok := splitAuthorityAndPath(clientID)
	if !ok {
		return false
	}
	// Scheme + authority are literal, so they must match byte for byte.
	if patternPrefix != clientPrefix {
		return false
	}

	// Split path from query/fragment on BOTH sides, with the same cut
	// validatePattern applies. Segmenting the raw remainder instead would let
	// a "/" inside a presented query act as a path separator: for the pattern
	// /oauth/*/client.json, a client_id of /oauth/authorize?x=/client.json
	// would split into three segments and match, even though its real path is
	// /oauth/authorize. The wildcard would then span a resource the catalog
	// never named.
	patternPath, patternSuffix := splitPathAndSuffix(patternRest)
	clientPath, clientSuffix := splitPathAndSuffix(clientRest)
	// Neither side may carry a wildcard here (validatePattern rejects one in
	// a query or fragment), so the suffixes compare literally.
	if patternSuffix != clientSuffix {
		return false
	}

	patternSegments := strings.Split(patternPath, "/")
	clientSegments := strings.Split(clientPath, "/")
	// A `*` never spans "/", so the segment count is fixed by the pattern.
	if len(patternSegments) != len(clientSegments) {
		return false
	}
	for i, segment := range patternSegments {
		if segment == "*" {
			// A wildcard segment must still BE a segment: empty would let
			// https://host//client.json match https://host/*/client.json.
			if clientSegments[i] == "" {
				return false
			}
			continue
		}
		if segment != clientSegments[i] {
			return false
		}
	}
	return true
}

// splitPathAndSuffix cuts the remainder returned by splitAuthorityAndPath
// into its path and its query/fragment suffix (the suffix keeps its leading
// "?" or "#", so an absent one is distinguishable from an empty one).
//
// Both validatePattern and matchesPattern must use this same cut. When they
// disagreed, a "/" inside a presented query was treated as a path separator
// and a wildcard matched one segment too shallow.
func splitPathAndSuffix(rest string) (string, string) {
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		return rest[:i], rest[i:]
	}
	return rest, ""
}

// splitAuthorityAndPath splits an https URL into its literal
// scheme+authority prefix and the remainder (path plus any query or
// fragment), without parsing or normalizing either half. Returns false for
// anything that is not https-shaped with a non-empty authority and a path.
func splitAuthorityAndPath(raw string) (string, string, bool) {
	const scheme = "https://"
	if !strings.HasPrefix(raw, scheme) {
		return "", "", false
	}
	rest := raw[len(scheme):]
	slash := strings.Index(rest, "/")
	if slash <= 0 {
		// No path, or an empty authority ("https:///x").
		return "", "", false
	}
	return raw[:len(scheme)+slash], rest[slash+1:], true
}
