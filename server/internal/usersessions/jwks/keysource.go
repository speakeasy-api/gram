package jwks

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

var (
	// ErrKeySourceURIInvalid reports a jwks_uri that is not acceptable as a
	// remote key set location.
	ErrKeySourceURIInvalid = errors.New("jwks_uri is not a valid https URL")

	// ErrKeySourceAmbiguous reports a client naming both an inline key set
	// and a jwks_uri, which leaves the authoritative set undefined
	// (RFC 7591 §2).
	ErrKeySourceAmbiguous = errors.New("jwks and jwks_uri must not both be present")

	// ErrKeySourceMissing reports a private_key_jwt client naming neither
	// key source, leaving nothing to verify its assertions against.
	ErrKeySourceMissing = errors.New("private_key_jwt requires jwks or jwks_uri")

	// ErrNoUsableSigningKey reports an inline key set none of whose keys
	// could ever verify an assertion.
	ErrNoUsableSigningKey = errors.New("key set contains no usable signing key")
)

// ValidateKeySource applies the key source rules every client registration
// surface shares, and returns the normalized inline key set to persist (a
// JSON null becomes absent). The rules apply whatever the authentication
// method: a public client may publish keys for other purposes, and a
// malformed key set is a broken registration either way. An asymmetric
// client must name exactly one source, or there is nothing to verify its
// assertions against; a private_key_jwt client's inline set must hold at
// least one usable signing key, since a set the resolver would parse to
// nothing registers a client that can never authenticate.
//
// The jwks_uri is checked for syntax only, never fetched: a fetch here would
// make registration depend on a third-party host and hand an unauthenticated
// caller an outbound request. Verification time resolves it, rate limited,
// and an unreachable set fails that client alone.
//
// Every rejection is one of this package's sentinel errors, reachable via
// errors.Is, so each caller can map them onto its own reason labels and
// client-facing descriptions.
func ValidateKeySource(method string, inline json.RawMessage, uri string) (json.RawMessage, error) {
	if IsNull(inline) {
		inline = nil
	}
	if err := ValidatePublicOnly(inline); err != nil {
		return nil, err
	}
	if uri != "" {
		if err := ValidateURI(uri); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrKeySourceURIInvalid, err)
		}
	}
	if inline != nil && uri != "" {
		return nil, ErrKeySourceAmbiguous
	}
	if method == oauthwire.AuthMethodPrivateKeyJWT {
		if inline == nil && uri == "" {
			return nil, ErrKeySourceMissing
		}
		if inline != nil {
			usable, err := UsableSigningKeys(inline)
			if err != nil {
				return nil, err
			}
			if usable == 0 {
				return nil, ErrNoUsableSigningKey
			}
		}
	}
	return inline, nil
}
