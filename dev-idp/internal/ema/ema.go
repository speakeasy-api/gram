// Package ema carries the wire vocabulary shared by the two halves of
// Enterprise-Managed Authorization: the IdP that mints an Identity Assertion
// JWT Authorization Grant (ID-JAG) and the resource authorization server that
// redeems one. (Okta ships the same profile under the name Cross-App Access;
// the wire format is identical, and ID-JAG is what actually appears in
// tokens and metadata either way.)
//
// The flow is the MCP Enterprise-Managed Authorization extension, profiling
// draft-ietf-oauth-identity-assertion-authz-grant:
//
//  1. the client signs in at the IdP and holds an id_token;
//  2. the client token-exchanges that at the IdP's /token for an ID-JAG
//     naming a specific resource authorization server in `aud`;
//  3. the client presents the ID-JAG at that server's /token under the
//     RFC 7523 jwt-bearer grant and gets an access token back.
//
// Everything here is the contract between steps 2 and 3, so it lives in one
// place rather than being spelled twice. The claim set is a declared struct
// so both halves agree on it by construction.
package ema

import (
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Protocol URNs. These strings are the interop surface -- an MCP client that
// speaks this profile sends and matches them literally.
//
// gosec's G101 heuristic reads "token"/"grant" in a constant name plus a long
// opaque-looking value as a leaked credential. Every value here is an
// IANA-registered URN published in an RFC, so the finding is suppressed
// per-line rather than the rule being switched off for the package.
const (
	// GrantTypeTokenExchange is the RFC 8693 grant the mint leg runs under.
	GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange" //nolint:gosec // G101: registered URN, not a credential

	// GrantTypeJWTBearer is the RFC 7523 grant the redeem leg runs under.
	GrantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer" //nolint:gosec // G101: registered URN, not a credential

	// TokenTypeIDJAG identifies an ID-JAG, both as the `requested_token_type`
	// on the mint request and the `issued_token_type` on its response.
	TokenTypeIDJAG = "urn:ietf:params:oauth:token-type:id-jag" //nolint:gosec // G101: registered URN, not a credential

	// TokenTypeIDToken and TokenTypeRefreshToken are the `subject_token_type`
	// values the mint leg accepts. The draft also lists a SAML 2 assertion
	// type, which the dev-idp has no way to produce and so does not accept.
	TokenTypeIDToken      = "urn:ietf:params:oauth:token-type:id_token"      //nolint:gosec // G101: registered URN, not a credential
	TokenTypeRefreshToken = "urn:ietf:params:oauth:token-type:refresh_token" //nolint:gosec // G101: registered URN, not a credential

	// GrantProfileIDJAG is advertised by a resource authorization server in
	// `authorization_grant_profiles_supported`. An MCP client checks for
	// exactly this value to decide whether a server speaks the profile.
	GrantProfileIDJAG = "urn:ietf:params:oauth:grant-profile:id-jag"

	// TokenTypeNotApplicable is the `token_type` on a mint response. An
	// ID-JAG is an authorization grant, not a credential to present at a
	// resource, so RFC 8693 has the IdP say so rather than claim "Bearer".
	TokenTypeNotApplicable = "N_A"

	// JWTType is the required `typ` header on an ID-JAG. A redeeming server
	// MUST check it, which is what stops an ordinary id_token from being
	// replayed into the jwt-bearer grant.
	JWTType = "oauth-id-jag+jwt"
)

// ResourceASPrefix is the path every resource authorization server is mounted
// under; the slug follows. Both halves need it -- the mint leg to turn an
// `audience` parameter back into a resource row, the redeem leg to know its
// own issuer identifier -- so it is declared once here.
const ResourceASPrefix = "/resource-as"

// ResourceASIssuer builds the canonical issuer identifier for a resource
// slug. Canonical means no trailing slash, matching RFC 8414.
func ResourceASIssuer(externalURL, slug string) string {
	return strings.TrimRight(externalURL, "/") + ResourceASPrefix + "/" + slug
}

// ResourceSlugFromIssuer parses an issuer identifier back into its resource
// slug, reporting false when the value does not name a resource
// authorization server on this dev-idp.
//
// A trailing slash is tolerated: the profile's own examples write the
// `audience` parameter with one, while RFC 8414 issuer identifiers carry
// none, and a caller should not have to guess which we meant.
func ResourceSlugFromIssuer(externalURL, issuer string) (string, bool) {
	base := strings.TrimRight(externalURL, "/") + ResourceASPrefix + "/"
	slug, ok := strings.CutPrefix(strings.TrimRight(issuer, "/"), base)
	if !ok || slug == "" || strings.Contains(slug, "/") {
		return "", false
	}
	return slug, true
}

// Claims is the ID-JAG claim set. `aud` is the issuer identifier of the
// resource authorization server that will redeem this grant, and `Resource`
// is the identifier of the protected resource behind it -- they are different
// URLs, and conflating them is the easiest way to get this profile wrong.
//
// `ClientID` is the requesting client's identifier at the redeeming server,
// which that server cross-checks against whoever actually authenticated on
// the redeem leg.
type Claims struct {
	Email    string `json:"email,omitempty"`
	Resource string `json:"resource,omitempty"`
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
	AuthTime int64  `json:"auth_time,omitempty"`
	jwt.RegisteredClaims
}

// ScopeList splits a space-delimited scope string into its members. OAuth
// scope strings are space-delimited by definition (RFC 6749 §3.3); this
// tolerates any run of whitespace.
func ScopeList(scope string) []string {
	return strings.Fields(scope)
}

// NarrowScope returns the members of `requested` that also appear in
// `permitted`, preserving the requested order, joined back into a scope
// string.
//
// An empty `permitted` means "no ceiling configured" and passes `requested`
// through untouched; an empty `requested` means the caller named no scopes
// and gets none. Those two empties mean opposite things, which is why this
// is one function rather than an inline intersection at each call site.
func NarrowScope(requested, permitted string) string {
	if strings.TrimSpace(permitted) == "" {
		return strings.Join(ScopeList(requested), " ")
	}
	allowed := ScopeList(permitted)
	kept := make([]string, 0, len(allowed))
	for _, s := range ScopeList(requested) {
		if slices.Contains(allowed, s) && !slices.Contains(kept, s) {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, " ")
}
