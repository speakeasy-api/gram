package remotesessions

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Issuer URL canonicalization, used by resolveRemoteSessionIssuer to decide
// whether two spellings name the same upstream authorization server.
//
// The canonical form lowercases the scheme and host, drops the scheme's default
// port, and strips trailing slashes. Everything else is left alone, and the
// omissions are the point: this function decides whether a customer silently
// lands on an issuer somebody else curated, so treating fewer things as equal
// fails safe. Over-matching attaches a tenant to the wrong upstream;
// under-matching costs one duplicate row.
//
// Deliberately treated as DISTINCT:
//
//   - http and https. Same host, different security properties, and an
//     authorization server reachable over both is a misconfiguration Gram should
//     not paper over.
//   - Path case. Hosts are case-insensitive per RFC 3986; paths are not, and
//     some issuers do route on path case.
//   - Percent-encoding variance in the path (%7E vs ~), repeated inner slashes,
//     and dot segments. Decoding or collapsing these means re-implementing RFC
//     3986 normalization, which is far more machinery than the payoff.
//   - Unicode vs punycode hosts. Equating them needs IDNA processing, whose
//     mapping rules are themselves a source of confusable-domain bugs.
//
// Rejected outright: a missing or non-http(s) scheme, a missing host, userinfo,
// a query string, and a fragment. RFC 8414 §2 requires an issuer identifier to
// be an https URL with no query or fragment, so anything carrying one is not an
// issuer identifier and should not become a lookup key.
//
// This is NOT issuerURLsEqual. That one compares a fetched metadata document's
// self-declared issuer against the URL Gram asked for, which is a trust check on
// a discovery response, and it stays strict on purpose. Widening it would let an
// upstream claim an identity it does not have. Keep the two separate.
type canonicalIssuerURL struct {
	// raw is the caller-supplied spelling with surrounding whitespace trimmed.
	// Kept so matchCandidates can probe for a stored row written exactly the way
	// this caller writes it.
	raw string

	scheme string
	// host is lowercased and, for an IPv6 literal, re-bracketed.
	host string
	// port is empty when the scheme's default port applies, so that
	// https://x and https://x:443 produce one canonical form.
	port string
	// path has trailing slashes stripped and may be empty.
	path string
}

// parseCanonicalIssuerURL validates raw as an issuer identifier and reduces it
// to its canonical parts.
func parseCanonicalIssuerURL(raw string) (canonicalIssuerURL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url is empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return canonicalIssuerURL{}, fmt.Errorf("parse issuer url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url must use http or https, got %q", u.Scheme)
	}
	if u.User != nil {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url must not carry userinfo")
	}
	// Both are checked on the raw string rather than on the parsed fields,
	// because the parsed fields do not reliably report an empty component:
	// net/url leaves Fragment and RawFragment empty for a bare "#", and the
	// query case only works at all because ForceQuery happens to exist. The
	// delimiters themselves are the dependable signal. A literal "?" or "#"
	// always starts a query or fragment, and an encoded one stays percent-
	// encoded in the path, so neither check can false-positive.
	if strings.Contains(trimmed, "?") {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url must not carry a query string")
	}
	if strings.Contains(trimmed, "#") {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url must not carry a fragment")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return canonicalIssuerURL{}, fmt.Errorf("issuer url has no host")
	}
	// url.Hostname strips the brackets from an IPv6 literal; put them back so the
	// canonical form is a URL that can be parsed again.
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}

	// An explicit default port is the same authority as no port at all, so drop
	// it. Any other port is significant and stays.
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}

	return canonicalIssuerURL{
		raw:    trimmed,
		scheme: scheme,
		host:   host,
		port:   port,
		path:   strings.TrimRight(u.EscapedPath(), "/"),
	}, nil
}

// String renders the canonical form. It is the advisory-lock key for a resolve,
// so two spellings that name one upstream also serialize against each other.
func (c canonicalIssuerURL) String() string {
	authority := c.host
	if c.port != "" {
		authority += ":" + c.port
	}

	return c.scheme + "://" + authority + c.path
}

// matchCandidates returns every literal spelling a stored issuer may carry that
// this URL should match.
//
// Lookup compares the raw `issuer` column against this set rather than
// normalizing the column, because the index it rides on
// (remote_session_issuers_issuer_idx) is on the raw column: wrapping `issuer` in
// any expression makes the index unusable and turns the lookup into a
// sequential scan. Expanding one canonical form back into its spellings keeps
// the query a handful of index probes.
//
// The consequence, and the accepted limitation of this whole approach, is that
// canonicalization applies to the SUPPLIED url only, never to what is already
// stored. A row written as https://LOGIN.example.com will not be found by a
// caller supplying https://login.example.com, and a duplicate is created
// instead. That is the safe direction to fail, and it is why the raw spelling is
// probed too: the common case of a second install discovering the same odd URL
// as the first still matches.
//
// The set is deliberately CLOSED at five entries and must stay that way. It
// covers exactly the two axes canonicalization collapses (trailing slash,
// default port) plus the caller's own spelling. Do not extend it to cover host
// case, punycode, or percent-encoding: those axes are unbounded, and enumerating
// them is a combinatorial explosion. If input-side matching ever proves
// insufficient, the fix is a stored canonical column that normalizes both sides,
// not a longer candidate list.
func (c canonicalIssuerURL) matchCandidates() []string {
	canonical := c.String()

	spellings := []string{canonical, canonical + "/"}

	// The default-port spellings exist only when no explicit port survived
	// canonicalization: those are precisely the URLs whose default port was
	// implicit and may have been written out in full when the row was stored. A
	// URL on an explicit non-default port has no such alternative spelling.
	if withPort := c.withDefaultPort(); withPort != "" {
		spellings = append(spellings, withPort, withPort+"/")
	}

	spellings = append(spellings, c.raw)

	candidates := make([]string, 0, len(spellings))
	for _, spelling := range spellings {
		if !slices.Contains(candidates, spelling) {
			candidates = append(candidates, spelling)
		}
	}

	return candidates
}

// withDefaultPort renders the canonical form with the scheme's default port
// written out explicitly, or "" when an explicit non-default port is present
// (in which case no default-port spelling of this URL exists).
func (c canonicalIssuerURL) withDefaultPort() string {
	if c.port != "" {
		return ""
	}

	port := "443"
	if c.scheme == "http" {
		port = "80"
	}

	return c.scheme + "://" + c.host + ":" + port + c.path
}
