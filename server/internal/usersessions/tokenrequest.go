// Token request handling for the user-session OAuth Authorization Server
// surface. Defines the RFC 6749 §4.1.3 (authorization_code) and §6
// (refresh_token) request shapes and the validation rules
// /mcp/{slug}/token enforces on each. Errors are reported as the shared
// *OAuthError (oautherror.go); the HTTP handler writes them as RFC 6749
// §5.2 JSON.

package usersessions

import (
	"net/url"
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
}

// AuthCodeTokenRequestFromForm decodes from url.Values (typically
// r.PostForm).
func AuthCodeTokenRequestFromForm(form url.Values) *AuthCodeTokenRequest {
	return &AuthCodeTokenRequest{
		Code:         form.Get("code"),
		RedirectURI:  form.Get("redirect_uri"),
		CodeVerifier: form.Get("code_verifier"),
	}
}

// SetDefaults is a no-op — all fields are required on this surface. Kept
// for symmetry with the other request types.
func (r *AuthCodeTokenRequest) SetDefaults() {}

// Validate checks the presence of each required field. Returns an
// *OAuthError on rejection. The redirect_uri match against the
// authorization grant and the PKCE verifier match against the stored
// code_challenge live in the handler (they require grant-side state).
func (r *AuthCodeTokenRequest) Validate() error {
	if r.Code == "" {
		return &OAuthError{Code: "invalid_request", Description: "code is required"}
	}
	if r.RedirectURI == "" {
		return &OAuthError{Code: "invalid_request", Description: "redirect_uri is required"}
	}
	if r.CodeVerifier == "" {
		return &OAuthError{Code: "invalid_request", Description: "code_verifier is required"}
	}
	return nil
}
