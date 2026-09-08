package clientauth

import "slices"

// Audiences is the pair of aud values one endpoint accepts on a client
// assertion, each computed per request from the resolved endpoint.
//
// Two values are accepted rather than one because the correct audience is
// genuinely ambiguous and the client cannot discover our preference. RFC 7523
// §3 and OIDC Core §9 say aud identifies the authorization server and that the
// token endpoint URL MAY be used; FAPI 2.0 and draft-ietf-oauth-rfc7523bis
// require the issuer identifier alone, and the libraries in the wild split
// the same way (Spring Security signs for the token URL, oauth4webapi for the
// issuer). RFC 8414 defines no metadata field naming which one a server
// wants, so pinning one would be a coin flip whose losing side is an opaque
// invalid_client.
//
// The endpoint value is the URL of the endpoint the request was posted to,
// and only that one: an assertion addressed to the revocation endpoint does
// not authenticate at the token endpoint, or the reverse. No client library
// has been observed addressing an assertion to a different endpoint than the
// one it posts to, so this costs no interoperability, and it removes the one
// cross-endpoint use an unspent assertion had.
//
// What neither value relaxes is cross-server replay, the dangerous case. Both
// are derived per request from the endpoint being addressed, never from a
// package-level constant, so an assertion minted for one MCP server does not
// verify at another.
type Audiences struct {
	// Issuer is the endpoint's issuer identifier.
	Issuer string

	// Endpoint is the URL of the endpoint the request was posted to.
	Endpoint string
}

// Match reports which accepted value the presented aud list names, if any.
// An assertion may carry several audiences; naming one accepted value is
// enough, which is what RFC 7523 §3 requires.
//
// The issuer identifier is checked first so that a server whose issuer is
// also one of its endpoint URLs reports the canonical label.
func (a Audiences) Match(presented []string) (AudienceKind, bool) {
	if a.Issuer != "" && slices.Contains(presented, a.Issuer) {
		return AudienceKindIssuer, true
	}
	if a.Endpoint != "" && slices.Contains(presented, a.Endpoint) {
		return AudienceKindEndpoint, true
	}
	return "", false
}

// accepted is the flat list of every value an assertion's aud may name, in
// the form the claim validator consumes. Empty entries are dropped so a
// caller that left a slot unset cannot accidentally admit an empty aud.
func (a Audiences) accepted() []string {
	values := make([]string, 0, 2)
	if a.Issuer != "" {
		values = append(values, a.Issuer)
	}
	if a.Endpoint != "" {
		values = append(values, a.Endpoint)
	}
	return values
}
