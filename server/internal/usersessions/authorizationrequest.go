// Authorization request handling for the user-session OAuth Authorization
// Server surface. Defines the RFC 6749 §4.1.1 request shape and the
// validation rules /mcp/{slug}/authorize enforces. Errors are reported as
// the shared *OAuthError (oautherror.go); the HTTP handler decides whether
// to surface them inline or via redirect — see the two-phase Validate
// methods below.

package usersessions

import (
	"fmt"
	"net/url"
	"slices"
)

// AuthorizationRequest is the RFC 6749 §4.1.1 authorization request, parsed
// from the /authorize query string. PKCE is mandatory on this surface:
// code_challenge + code_challenge_method MUST be supplied.
type AuthorizationRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// AuthorizationRequestFromQuery decodes an AuthorizationRequest from
// url.Values (typically r.URL.Query()).
func AuthorizationRequestFromQuery(q url.Values) *AuthorizationRequest {
	return &AuthorizationRequest{
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		ResponseType:        q.Get("response_type"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
	}
}

// SetDefaults applies RFC 6749 §4.1.1 defaults. response_type and
// code_challenge_method are both REQUIRED on this surface (no spec-defined
// default), so this is presently a no-op — kept for symmetry with the
// other request types and to give future spec additions a place to land.
func (r *AuthorizationRequest) SetDefaults() {}

// ValidateRedirectableFields checks the fields the AS MUST validate
// BEFORE it can safely redirect any error back to the caller. Per RFC
// 6749 §4.1.2.1, an unknown client_id or invalid redirect_uri means we
// can't trust the URI we'd redirect to — these errors MUST be surfaced
// inline. Callers should run this first; if it returns a *OAuthError,
// write it as an HTTP-level response and stop.
func (r *AuthorizationRequest) ValidateRedirectableFields() error {
	if r.ClientID == "" {
		return &OAuthError{Code: "invalid_request", Description: "client_id is required"}
	}
	if r.RedirectURI == "" {
		return &OAuthError{Code: "invalid_request", Description: "redirect_uri is required"}
	}
	return nil
}

// ValidatePostRedirect checks the remaining fields, assumed to run AFTER
// the redirect_uri has been validated against the registered client.
// Errors here MAY be redirected back to the client per RFC 6749 §4.1.2.1
// (the current handler still surfaces them inline; that's a forward-
// compatible choice).
func (r *AuthorizationRequest) ValidatePostRedirect() error {
	if !slices.Contains(SupportedResponseTypes, r.ResponseType) {
		return &OAuthError{Code: "unsupported_response_type", Description: fmt.Sprintf("response_type must be one of %v", SupportedResponseTypes)}
	}
	if r.CodeChallenge == "" {
		return &OAuthError{Code: "invalid_request", Description: "code_challenge is required (PKCE mandatory)"}
	}
	if !slices.Contains(SupportedCodeChallengeMethods, r.CodeChallengeMethod) {
		return &OAuthError{Code: "invalid_request", Description: fmt.Sprintf("unsupported code_challenge_method %q", r.CodeChallengeMethod)}
	}
	return nil
}
