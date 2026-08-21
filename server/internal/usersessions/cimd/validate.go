package cimd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// validationError pairs a validation rejection with the machine-readable
// reason label recorded on cimd.validation.failures. Unwrap keeps the
// *oauthwire.Error (or a wrapped equivalent) reachable via errors.As, so
// callers' wire-format mapping is unaffected by the annotation.
type validationError struct {
	reason validationReason
	err    error
}

func (e *validationError) Error() string { return e.err.Error() }
func (e *validationError) Unwrap() error { return e.err }

// oauthValidationError builds the standard rejection shape: a client-safe
// OAuth error annotated with its metric reason.
func oauthValidationError(reason validationReason, code string, description string) error {
	return &validationError{reason: reason, err: &oauthwire.Error{Code: code, Description: description}}
}

// validationReasonOf extracts the reason label from a validation rejection,
// or "" when the error did not come from a validate function.
func validationReasonOf(err error) validationReason {
	if ve, ok := errors.AsType[*validationError](err); ok {
		return ve.reason
	}
	return ""
}

// ValidateClientIDURL enforces the -02 §3 Client Identifier URL syntax rules
// on the presented client_id and returns the parsed URL.
//
// Exported so the management API can apply exactly these rules when an
// operator adds a custom CIMD URL to an issuer: a URL that would be
// rejected at authorization time is worth rejecting at configuration time,
// where the error is actionable, rather than storing a dead policy entry.
//
// Rules:
//
//   - https scheme (IsClientIDURL gates entry, so a violation here means a
//     mixed-case or otherwise mangled prefix)
//   - no userinfo component
//   - a path component is required (bare https://example.com is invalid)
//   - no "." or ".." path segments
//   - no fragment
//   - a query string is tolerated (clients SHOULD NOT use one)
//
// No normalization is applied anywhere — -02 §3 mandates simple string
// comparison per RFC 3986 §6.2.1, so https://example.com/c and
// https://example.com:443/c are different client identifiers.
func ValidateClientIDURL(clientID string) (*url.URL, error) {
	if len(clientID) > maxClientIDLength {
		return nil, oauthValidationError(reasonClientIDTooLong, "invalid_request", fmt.Sprintf("client_id exceeds the %d byte limit", maxClientIDLength))
	}
	if !strings.HasPrefix(clientID, "https://") {
		return nil, oauthValidationError(reasonClientIDScheme, "invalid_request", "client_id URL must use the https scheme")
	}
	// url.Parse tolerates fragments by splitting them off; detect them on
	// the raw string so an empty "#" is caught too.
	if strings.Contains(clientID, "#") {
		return nil, oauthValidationError(reasonClientIDFragment, "invalid_request", "client_id URL must not contain a fragment")
	}

	parsed, err := url.Parse(clientID)
	if err != nil {
		return nil, oauthValidationError(reasonClientIDUnparseable, "invalid_request", "client_id is not a valid URL")
	}
	if parsed.User != nil {
		return nil, oauthValidationError(reasonClientIDUserinfo, "invalid_request", "client_id URL must not contain a userinfo component")
	}
	if parsed.Host == "" {
		return nil, oauthValidationError(reasonClientIDMissingHost, "invalid_request", "client_id URL must include a host")
	}
	if parsed.Path == "" {
		return nil, oauthValidationError(reasonClientIDMissingPath, "invalid_request", "client_id URL must include a path component")
	}
	for segment := range strings.SplitSeq(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, oauthValidationError(reasonClientIDDotSegments, "invalid_request", `client_id URL must not contain "." or ".." path segments`)
		}
	}

	return parsed, nil
}

// validationFindings carries the non-blocking observations validateDocument
// made about a document it accepted. Only meaningful alongside a nil error;
// every rejection returns the zero value.
type validationFindings struct {
	// crossOriginRedirectOrigins lists the distinct origins (scheme://host,
	// compared without normalization) of non-loopback redirect_uris that do
	// not share the client_id URL's origin, in first-seen order. Cross-origin
	// redirects are accepted — the load-bearing control is exact-match
	// validation of the authorization request's redirect_uri against the
	// document at authorize time — so this exists purely to feed the
	// cimd.redirect_uris.cross_origin counter and its log line.
	crossOriginRedirectOrigins []string
}

// validateDocument applies the -02 §4 document rules plus local policy to a
// parsed document. clientID is both the client_id the OAuth client presented
// and the URL that was fetched (the fetcher never follows redirects), so the
// single equality check here completes the §4 triple equality requirement.
func validateDocument(doc *Document, clientID string, clientIDURL *url.URL) (validationFindings, error) {
	var none validationFindings
	if doc.ClientID != clientID {
		return none, oauthValidationError(reasonClientIDMismatch, "invalid_client_metadata", "document client_id does not match the client identifier URL")
	}
	// MCP requires client_name, and the user_session_clients.client_name
	// column is NOT NULL — requiring it here avoids fallback logic.
	if doc.ClientName == "" {
		return none, oauthValidationError(reasonMissingClientName, "invalid_client_metadata", "client_name is required")
	}
	if len(doc.ClientName) > maxClientNameLength {
		return none, oauthValidationError(reasonClientNameTooLong, "invalid_client_metadata", fmt.Sprintf("client_name exceeds the %d byte limit", maxClientNameLength))
	}
	// Only public clients are accepted. -02 §8.2's direction of travel is
	// that capable clients SHOULD authenticate (MCP clients MAY use
	// private_key_jwt); accepting those is deferred, and when it lands
	// §8.2's RFC 7523 §2.2 enforcement MUST come with it.
	//
	// An ABSENT value is treated as "none", not rejected. The field is not
	// required by -02 (only client_id is) and RFC 7591's
	// client_secret_basic default cannot apply here, because §4.1 forbids a
	// CIMD document from using client_secret_post, client_secret_basic,
	// client_secret_jwt, or any other shared-symmetric-secret method. So
	// absence cannot mean a secret-bearing client, and the only readings
	// left are "none" or an explicit asymmetric method — which would have
	// to say so.
	//
	// This is load-bearing for real clients, not a theoretical nicety:
	// OpenAI's documents (both ChatGPT's and Codex CLI's) omit the field
	// entirely while being plain public clients, and rejecting them here
	// would make any preset for them dead on arrival.
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
		return none, oauthValidationError(reasonInvalidAuthMethod, "invalid_client_metadata", `token_endpoint_auth_method must be "none" or absent`)
	}
	// -02 §4.1: a metadata document is public, so it must never carry a
	// client secret in any form. Presence alone invalidates the document.
	if doc.ClientSecret != nil || doc.ClientSecretExpiresAt != nil {
		return none, oauthValidationError(reasonContainsSecret, "invalid_client_metadata", "document must not contain client_secret or client_secret_expires_at")
	}
	if err := validateJWKSPublicOnly(doc.JWKS); err != nil {
		return none, err
	}

	if len(doc.RedirectURIs) == 0 {
		return none, oauthValidationError(reasonMissingRedirectURIs, "invalid_redirect_uri", "redirect_uris is required")
	}
	if len(doc.RedirectURIs) > maxRedirectURIs {
		return none, oauthValidationError(reasonTooManyRedirectURIs, "invalid_redirect_uri", fmt.Sprintf("redirect_uris exceeds the limit of %d entries", maxRedirectURIs))
	}
	var crossOrigins []string
	for _, uri := range doc.RedirectURIs {
		if len(uri) > maxRedirectURILength {
			return none, oauthValidationError(reasonRedirectURITooLong, "invalid_redirect_uri", fmt.Sprintf("redirect_uri exceeds the %d byte limit", maxRedirectURILength))
		}
		// ValidateRedirectURI returns *oauthwire.Error values of its
		// own, so the wrap preserves the client-safe wire mapping while the
		// annotation adds the shared metric reason.
		if err := oauthwire.ValidateRedirectURI(uri); err != nil {
			return none, &validationError{reason: reasonRedirectURIInvalid, err: fmt.Errorf("validate redirect_uri: %w", err)}
		}
		parsed, err := url.Parse(uri)
		if err != nil {
			// Unreachable in practice: ValidateRedirectURI parses the same
			// string above. Kept so a divergence between the two parses
			// fails closed instead of skipping the scheme rule.
			return none, oauthValidationError(reasonRedirectURIInvalid, "invalid_redirect_uri", "redirect_uri must be an absolute URL")
		}
		if IsLoopbackRedirectURI(parsed) {
			continue
		}
		// Non-loopback redirect URIs must use https. This is stricter than
		// oauthwire.ValidateRedirectURI, which deliberately tolerates
		// native-app custom schemes for DCR clients (AIS-434): a CIMD
		// document is fetched from an https URL and self-asserts its
		// registration, so the redirect set is held to web-grade schemes
		// until a concrete client needs otherwise.
		if parsed.Scheme != "https" {
			return none, oauthValidationError(reasonRedirectURIScheme, "invalid_redirect_uri", fmt.Sprintf("redirect_uri %q must use the https scheme or be an RFC 8252 loopback http URL", uri))
		}
		// Cross-origin redirect URIs are accepted (no surveyed CIMD
		// implementation binds redirect origins to the client_id origin, and
		// -02 §8.1 leaves it optional) but observed: the origins feed a
		// counter and log line so a preset vendor moving to cross-origin
		// redirects is visible. Both URLs are https by this point, so the
		// origin comparison reduces to the host — compared without
		// normalization, so an explicit :443 is a different origin, and an
		// https URL on a loopback host counts as cross-origin too
		// (IsLoopbackRedirectURI exempts only http, per RFC 8252 §7.3).
		if parsed.Host != clientIDURL.Host {
			origin := parsed.Scheme + "://" + parsed.Host
			if !slices.Contains(crossOrigins, origin) {
				crossOrigins = append(crossOrigins, origin)
			}
		}
	}

	return validationFindings{crossOriginRedirectOrigins: crossOrigins}, nil
}

// IsLoopbackRedirectURI reports whether a parsed redirect URI is an RFC 8252
// §7.3 loopback redirect: http scheme with a host of 127.0.0.1, ::1, or
// localhost, on any port. Shared with the mcp package's request-time
// redirect matching, where RFC 8252 requires variable loopback ports to be
// accepted for native apps.
func IsLoopbackRedirectURI(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

// validateJWKSPublicOnly rejects a jwks member containing private or
// symmetric key material (-02 §4.1 — new in -02). A public metadata document
// carrying a private key would let anyone impersonate the client, so the
// whole document is invalid. Detection keys on the JWK members that only
// appear on non-public keys: "d" (RSA / EC / OKP private component) and "k"
// (symmetric oct key).
func validateJWKSPublicOnly(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}

	var jwks struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &jwks); err != nil {
		return oauthValidationError(reasonJWKSInvalid, "invalid_client_metadata", "jwks is not a valid JWK Set")
	}
	for _, key := range jwks.Keys {
		if _, ok := key["d"]; ok {
			return oauthValidationError(reasonJWKSPrivateKey, "invalid_client_metadata", "jwks must not contain private key material")
		}
		if _, ok := key["k"]; ok {
			return oauthValidationError(reasonJWKSSymmetricKey, "invalid_client_metadata", "jwks must not contain symmetric key material")
		}
	}
	return nil
}
