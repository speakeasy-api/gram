// Client registration logic for the user-session OAuth Authorization Server
// surface. Defines the RFC 7591 §3.1 request shape and the validate /
// defaults rules that determine which clients are accepted by
// /mcp/{slug}/register. Errors are reported as the shared *OAuthError
// (oautherror.go).
//
// The mcp package's HandleRegister handler wraps this with HTTP plumbing
// (Content-Type sniffing, body cap, response writing). The supported sets
// declared here are advertised verbatim in the AS metadata document so
// registered clients can only request what the AS will accept.

package usersessions

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// SupportedGrantTypes / SupportedResponseTypes / SupportedAuthMethods /
// SupportedCodeChallengeMethods are the OAuth values the user-session AS
// supports. Mirrored into the RFC 8414 metadata document by
// mcp.HandleGetAuthorizationServer; enforced on the /register and
// /authorize handlers by the typed request Validate methods.
var (
	SupportedGrantTypes    = []string{"authorization_code", "refresh_token"}
	SupportedResponseTypes = []string{"code"}
	// `none` covers public PKCE-only clients (mobile, CLI, MCP SDK). Real
	// MCP clients in the wild use it. PKCE provides per-flow integrity; the
	// only guard against cross-flow client-id confusion is the consent
	// prompt itself, which we always render (HandleConsent never skips).
	SupportedAuthMethods          = []string{"client_secret_basic", "client_secret_post", "none"}
	SupportedCodeChallengeMethods = []string{"S256"}
)

// RegistrationRequest is the RFC 7591 §3.1 client metadata document. Only
// the fields we honour are listed; unknown fields are ignored.
//
// `scope` is intentionally absent: RFC 7591 §3.2.1 requires the registration
// response to reflect actually-registered metadata, and we have no scope
// enforcement to back it up — echoing a `scope` field would assert server
// state we don't hold. Add it back when we ship a scope-aware /token.
type RegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// SetDefaults populates the RFC 7591 §2 defaults for fields the client
// didn't supply. Must be called before Validate so the §2.1 grant/response
// correlation check sees materialized values.
func (r *RegistrationRequest) SetDefaults() {
	if len(r.GrantTypes) == 0 {
		r.GrantTypes = []string{"authorization_code"}
	}
	if len(r.ResponseTypes) == 0 {
		r.ResponseTypes = []string{"code"}
	}
	if r.TokenEndpointAuthMethod == "" {
		r.TokenEndpointAuthMethod = "client_secret_basic"
	}
}

// Validate checks the (defaulted) fields of an RFC 7591 §3.1 client metadata
// document. Returns an *OAuthError on a spec-defined rejection. Callers
// must invoke SetDefaults first so grant_types / response_types / auth
// method are populated.
func (r *RegistrationRequest) Validate() error {
	if r.ClientName == "" {
		return &OAuthError{Code: "invalid_client_metadata", Description: "client_name is required"}
	}
	if len(r.RedirectURIs) == 0 {
		return &OAuthError{Code: "invalid_redirect_uri", Description: "redirect_uris is required"}
	}
	for _, u := range r.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return err
		}
	}
	for _, gt := range r.GrantTypes {
		if !slices.Contains(SupportedGrantTypes, gt) {
			return &OAuthError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported grant_type %q", gt)}
		}
	}
	for _, rt := range r.ResponseTypes {
		if !slices.Contains(SupportedResponseTypes, rt) {
			return &OAuthError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported response_type %q", rt)}
		}
	}
	if !slices.Contains(SupportedAuthMethods, r.TokenEndpointAuthMethod) {
		return &OAuthError{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported token_endpoint_auth_method %q", r.TokenEndpointAuthMethod)}
	}

	// RFC 7591 §2.1 correlation: response_type "code" requires grant_type
	// "authorization_code" and vice versa.
	hasCodeResponse := slices.Contains(r.ResponseTypes, "code")
	hasAuthCodeGrant := slices.Contains(r.GrantTypes, "authorization_code")
	if hasCodeResponse && !hasAuthCodeGrant {
		return &OAuthError{Code: "invalid_client_metadata", Description: `response_type "code" requires grant_type "authorization_code"`}
	}
	if hasAuthCodeGrant && !hasCodeResponse {
		return &OAuthError{Code: "invalid_client_metadata", Description: `grant_type "authorization_code" requires response_type "code"`}
	}
	// refresh_token can only follow an initial authorization_code in our
	// supported set; a client registering refresh_token alone has no way
	// to ever obtain one.
	if slices.Contains(r.GrantTypes, "refresh_token") && !hasAuthCodeGrant {
		return &OAuthError{Code: "invalid_client_metadata", Description: `grant_type "refresh_token" requires grant_type "authorization_code"`}
	}
	return nil
}

// validateRedirectURI enforces the OAuth 2.1 / RFC 8252 redirect-URI scheme
// rules:
//
//   - https://... for web + confidential clients.
//   - http://... only when the host is a loopback literal (127.0.0.1, ::1,
//     localhost) per RFC 8252 §7.3.
//   - custom-scheme://... for native apps, restricted per RFC 8252 §7.1:
//     scheme must contain a "." (reverse-DNS form) to make collisions
//     between independent apps unlikely.
//
// Dangerous schemes (javascript:, data:, vbscript:, file:, blob:, etc.)
// are rejected unconditionally — they would let a registered redirect_uri
// turn the AS's 302 Location into an XSS or local-file fetch vector.
func validateRedirectURI(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return &OAuthError{Code: "invalid_redirect_uri", Description: "redirect_uri must be an absolute URL"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https":
		if parsed.Host == "" {
			return &OAuthError{Code: "invalid_redirect_uri", Description: "redirect_uri must include a host"}
		}
		return nil
	case "http":
		// RFC 8252 §7.3 loopback hosts only. parsed.Hostname() strips any
		// :port suffix.
		host := strings.ToLower(parsed.Hostname())
		switch host {
		case "127.0.0.1", "::1", "localhost":
			return nil
		default:
			return &OAuthError{Code: "invalid_redirect_uri", Description: "http redirect_uri is only allowed for loopback hosts (127.0.0.1, ::1, localhost)"}
		}
	default:
		// Native-app custom scheme. RFC 8252 §7.1 recommends reverse-DNS
		// form (e.g. com.example.app); require a "." in the scheme to make
		// inter-app collisions unlikely. Reject the well-known dangerous
		// schemes explicitly even if a future spec extension would allow
		// them through the dot rule.
		switch scheme {
		case "javascript", "data", "vbscript", "file", "blob", "view-source":
			return &OAuthError{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uri scheme %q is not permitted", scheme)}
		}
		if !strings.Contains(scheme, ".") {
			return &OAuthError{Code: "invalid_redirect_uri", Description: fmt.Sprintf("redirect_uri custom scheme %q must be in reverse-DNS form (RFC 8252 §7.1)", scheme)}
		}
		return nil
	}
}
