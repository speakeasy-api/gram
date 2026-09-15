// Token request handling for the user-session OAuth Authorization Server
// surface. Defines the RFC 6749 §4.1.3 (authorization_code) and §6
// (refresh_token) request shapes and the validation rules
// /mcp/{slug}/token enforces on each. Errors are reported as the shared
// *oauthwire.Error; the HTTP handler writes them as RFC 6749
// §5.2 JSON.

package usersessions

import (
	"net/url"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// AuthCodeTokenRequest is the RFC 6749 §4.1.3 token request issued by a
// client exchanging an authorization code for a token pair. PKCE is
// mandatory on this surface — the code_verifier is required, and the
// /token handler matches it against the code_challenge stored on the
// authorization grant.
type AuthCodeTokenRequest struct {
	Code         string
	RedirectURI  string
	CodeVerifier string

	// Resources holds every RFC 8707 `resource` indicator submitted, naming the
	// MCP server the token is being requested for. Empty when the parameter was
	// absent, which stays acceptable; every value is validated against the
	// addressed endpoint's canonical URI by
	// oauthwire.ValidateResourceIndicators in the /token handler.
	Resources []string
}

// AuthCodeTokenRequestFromForm decodes from url.Values (typically
// r.PostForm).
func AuthCodeTokenRequestFromForm(form url.Values) *AuthCodeTokenRequest {
	return &AuthCodeTokenRequest{
		Code:         form.Get("code"),
		RedirectURI:  form.Get("redirect_uri"),
		CodeVerifier: form.Get("code_verifier"),
		Resources:    oauthwire.ResourceIndicatorsFrom(form),
	}
}

// SetDefaults is a no-op — all fields are required on this surface. Kept
// for symmetry with the other request types.
func (r *AuthCodeTokenRequest) SetDefaults() {}

// Validate checks the presence of each required field. Returns an
// *oauthwire.Error on rejection. The redirect_uri match against the
// authorization grant and the PKCE verifier match against the stored
// code_challenge live in the handler (they require grant-side state).
func (r *AuthCodeTokenRequest) Validate() error {
	if r.Code == "" {
		return &oauthwire.Error{Code: "invalid_request", Description: "code is required"}
	}
	if r.RedirectURI == "" {
		return &oauthwire.Error{Code: "invalid_request", Description: "redirect_uri is required"}
	}
	if r.CodeVerifier == "" {
		return &oauthwire.Error{Code: "invalid_request", Description: "code_verifier is required"}
	}
	return nil
}

// RefreshTokenRequest is the RFC 6749 §6 token request issued by a client
// rotating its refresh token. The scope parameter is intentionally absent
// — see usersessions.RegistrationRequest's comment on un-persisted scope
// state; the /token response likewise doesn't echo scope.
type RefreshTokenRequest struct {
	RefreshToken string

	// Resources holds every RFC 8707 `resource` indicator submitted, naming the
	// MCP server the rotated token is being requested for. MCP 2026-07-28 has
	// clients send the parameter on every token request, refreshes included.
	// Empty when it was absent, which stays acceptable; every value is validated
	// against the addressed endpoint's canonical URI by
	// oauthwire.ValidateResourceIndicators in the /token handler.
	Resources []string
}

// RefreshTokenRequestFromForm decodes from url.Values (typically
// r.PostForm).
func RefreshTokenRequestFromForm(form url.Values) *RefreshTokenRequest {
	return &RefreshTokenRequest{
		RefreshToken: form.Get("refresh_token"),
		Resources:    oauthwire.ResourceIndicatorsFrom(form),
	}
}

// SetDefaults is a no-op — refresh_token is required. Kept for symmetry
// with the other request types.
func (r *RefreshTokenRequest) SetDefaults() {}

// Validate checks the presence of refresh_token. Returns an *oauthwire.Error
// on rejection. Hash lookup + client-binding verification + expiry check
// live in the handler since they require database state.
func (r *RefreshTokenRequest) Validate() error {
	if r.RefreshToken == "" {
		return &oauthwire.Error{Code: "invalid_request", Description: "refresh_token is required"}
	}
	return nil
}
