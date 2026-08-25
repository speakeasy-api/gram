// Client registration logic for the user-session OAuth Authorization Server
// surface. Defines the RFC 7591 §3.1 request shape and the validate /
// defaults rules that determine which clients are accepted by
// /mcp/{slug}/register. Errors are reported as the shared
// *oauthwire.Error.
//
// The mcp package's HandleRegister handler wraps this with HTTP plumbing
// (Content-Type sniffing, body cap, response writing). The supported sets
// declared here are advertised verbatim in the AS metadata document so
// registered clients can only request what the AS will accept.

package usersessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// SupportedGrantTypes / SupportedResponseTypes / SupportedAuthMethods /
// SupportedCodeChallengeMethods are the OAuth values the user-session AS
// supports. Mirrored into the RFC 8414 metadata document by
// mcp.HandleGetAuthorizationServer; enforced on the /register and
// /authorize handlers by the typed request Validate methods.
var (
	SupportedGrantTypes    = []string{"authorization_code", "refresh_token"}
	SupportedResponseTypes = []string{"code"}

	// SupportedAuthMethods is the user-session AS's own accepted
	// token_endpoint_auth_method set, and is not shared policy: the other
	// authorization servers that reuse RegistrationRequest declare their own
	// and pass it to Validate. A method added here therefore changes this
	// server alone.
	//
	// `none` covers public PKCE-only clients (mobile, CLI, MCP SDK). Real
	// MCP clients in the wild use it. PKCE provides per-flow integrity; the
	// only guard against cross-flow client-id confusion is the consent
	// prompt itself, which we always render (HandleConsent never skips).
	//
	// `private_key_jwt` is the one method that proves possession of a key
	// never sent to the server. A registration declaring it must supply the
	// key source, and the token endpoint holds it to an RFC 7523 §2.2
	// assertion from then on.
	SupportedAuthMethods = []string{oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost, oauthwire.AuthMethodNone, oauthwire.AuthMethodPrivateKeyJWT}

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

	// JWKS is the client's inline public key set (RFC 7591 §2). Screened
	// for private or symmetric material whatever the method, and required,
	// in exactly one of this and JWKSURI, for private_key_jwt.
	JWKS json.RawMessage `json:"jwks,omitempty"`

	// JWKSURI is the https location of the client's public key set, the
	// remote alternative to JWKS. RFC 7591 §2 forbids supplying both.
	JWKSURI string `json:"jwks_uri,omitempty"`
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
		r.TokenEndpointAuthMethod = oauthwire.AuthMethodClientSecretBasic
	}
}

// Validate checks the (defaulted) fields of an RFC 7591 §3.1 client metadata
// document. Returns an *oauthwire.Error on a spec-defined rejection. Callers
// must invoke SetDefaults first so grant_types / response_types / auth
// method are populated.
//
// supportedAuthMethods is the caller's accepted token_endpoint_auth_method
// set rather than a package-level list, because several authorization servers
// share this request type while accepting different methods: a shared list
// would let a method added for one server start being accepted by the others
// without anyone deciding that. Pass the same slice the server advertises as
// token_endpoint_auth_methods_supported, so what it accepts and what it
// advertises cannot drift apart.
func (r *RegistrationRequest) Validate(supportedAuthMethods []string) error {
	if r.ClientName == "" {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: "client_name is required"}
	}
	if len(r.RedirectURIs) == 0 {
		return &oauthwire.Error{Code: "invalid_redirect_uri", Description: "redirect_uris is required"}
	}
	for _, u := range r.RedirectURIs {
		if err := oauthwire.ValidateRedirectURI(u); err != nil {
			// Wrapped rather than returned bare, but the wrapped value is
			// still an *oauthwire.Error: every handler resolves it with errors.As,
			// so the client-facing code and description are unchanged.
			return fmt.Errorf("validate redirect_uri: %w", err)
		}
	}
	for _, gt := range r.GrantTypes {
		if !slices.Contains(SupportedGrantTypes, gt) {
			return &oauthwire.Error{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported grant_type %q", gt)}
		}
	}
	for _, rt := range r.ResponseTypes {
		if !slices.Contains(SupportedResponseTypes, rt) {
			return &oauthwire.Error{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported response_type %q", rt)}
		}
	}
	if !slices.Contains(supportedAuthMethods, r.TokenEndpointAuthMethod) {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: fmt.Sprintf("unsupported token_endpoint_auth_method %q", r.TokenEndpointAuthMethod)}
	}

	// Key source rules are shared with CIMD validation; only the wire
	// description is decided here.
	normalizedJWKS, err := jwks.ValidateKeySource(r.TokenEndpointAuthMethod, r.JWKS, r.JWKSURI)
	if err != nil {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: keySourceDescription(err)}
	}
	r.JWKS = normalizedJWKS

	// RFC 7591 §2.1 correlation: response_type "code" requires grant_type
	// "authorization_code" and vice versa.
	hasCodeResponse := slices.Contains(r.ResponseTypes, "code")
	hasAuthCodeGrant := slices.Contains(r.GrantTypes, "authorization_code")
	if hasCodeResponse && !hasAuthCodeGrant {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: `response_type "code" requires grant_type "authorization_code"`}
	}
	if hasAuthCodeGrant && !hasCodeResponse {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: `grant_type "authorization_code" requires response_type "code"`}
	}
	// refresh_token can only follow an initial authorization_code in our
	// supported set; a client registering refresh_token alone has no way
	// to ever obtain one.
	if slices.Contains(r.GrantTypes, "refresh_token") && !hasAuthCodeGrant {
		return &oauthwire.Error{Code: "invalid_client_metadata", Description: `grant_type "refresh_token" requires grant_type "authorization_code"`}
	}
	return nil
}

// keySourceDescription is the client-facing description for a key source
// rejection.
func keySourceDescription(err error) string {
	switch {
	case errors.Is(err, jwks.ErrPrivateKeyMaterial):
		return "jwks must not contain private key material"
	case errors.Is(err, jwks.ErrSymmetricKeyMaterial):
		return "jwks must not contain symmetric key material"
	case errors.Is(err, jwks.ErrKeySourceURIInvalid):
		return "jwks_uri is not a valid https URL"
	case errors.Is(err, jwks.ErrKeySourceAmbiguous):
		return "jwks and jwks_uri must not both be present"
	case errors.Is(err, jwks.ErrKeySourceMissing):
		return "private_key_jwt requires jwks or jwks_uri"
	case errors.Is(err, jwks.ErrNoUsableSigningKey):
		return "jwks contains no usable signing key"
	default:
		return "jwks is not a valid JWK Set"
	}
}
