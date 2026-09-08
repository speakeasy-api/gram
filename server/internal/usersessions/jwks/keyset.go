package jwks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-jose/go-jose/v4"
)

var (
	// ErrKeySetInvalid reports a document that is not a parseable JWK Set.
	ErrKeySetInvalid = errors.New("not a valid JWK Set")

	// ErrPrivateKeyMaterial reports a key set containing a private key
	// component. A published key set carrying a private key would let anyone
	// impersonate its owner, so the whole set is rejected rather than the
	// one key skipped.
	ErrPrivateKeyMaterial = errors.New("key set contains private key material")

	// ErrSymmetricKeyMaterial reports a key set containing a symmetric (oct)
	// key. Symmetric keys have no place in a published key set for the same
	// reason HS* algorithms are off the allowlist: possession of the
	// verification key is possession of the signing key.
	ErrSymmetricKeyMaterial = errors.New("key set contains symmetric key material")

	// ErrKeyNotFound reports that no usable key in the set matches the
	// requested kid (or, for a kid-less request, that the set does not
	// contain exactly one usable key). After a KeyResolver has consulted the
	// upstream within its refresh policy, this is the terminal answer for an
	// assertion signed with an unknown key.
	ErrKeyNotFound = errors.New("no verification key matches")
)

// ValidatePublicOnly rejects a JWK Set document containing private or
// symmetric key material. Detection keys on the JWK members that only appear
// on non-public keys — "d" (RSA / EC / OKP private component), the RFC 7518
// §6.3.2 RSA CRT and additional-primes members (p, q, dp, dq, qi, oth), and
// "k" (symmetric oct key) — over the raw JSON, so a malformed key cannot
// smuggle material past a stricter parser by failing it. A nil document is
// accepted: absence of a key set is the caller's policy question, not a
// validation failure.
//
// Shared with the cimd package, whose -02 §4.1 document screening is the same
// rule with its own wire-format wrapping.
func ValidatePublicOnly(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	if containsNULEscape(raw) {
		return fmt.Errorf("key set contains a NUL escape: %w", ErrKeySetInvalid)
	}

	// The pointer distinguishes an absent or null "keys" member from an
	// empty array. RFC 7517 §5 makes "keys" the set's one required member,
	// and parseKeySet applies the same rule at resolve time: a document
	// accepted here that parseKeySet would refuse would register a client
	// that can never authenticate.
	var keySet struct {
		Keys *[]map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &keySet); err != nil {
		return fmt.Errorf("parse key set: %w", ErrKeySetInvalid)
	}
	if keySet.Keys == nil {
		return fmt.Errorf(`key set missing "keys" array: %w`, ErrKeySetInvalid)
	}
	for _, key := range *keySet.Keys {
		if err := screenKeyMaterial(key); err != nil {
			return err
		}
	}
	return nil
}

// IsNull reports whether a raw JSON member is the literal null. Go's decoder
// stores an explicit `"jwks": null` into a json.RawMessage as the four bytes
// "null" rather than as a nil slice, so a presence check on the slice alone
// would count it as a supplied key set. Callers applying an exactly-one rule
// to jwks and jwks_uri treat it as absent.
func IsNull(raw json.RawMessage) bool {
	return len(raw) > 0 && string(bytes.TrimSpace(raw)) == "null"
}

// containsNULEscape reports whether a JSON document spells a NUL character as
// the six-character escape backslash-u-0000. Go's decoder accepts it, but Postgres
// refuses it inside a jsonb value, so a key set carrying one would pass every
// parser here and then fail at the write, as a 500 on an unauthenticated
// endpoint. No legitimate key set has a NUL in any member, so the document is
// simply invalid.
func containsNULEscape(raw json.RawMessage) bool {
	for i := 0; i+6 <= len(raw); i++ {
		if raw[i] != '\\' || raw[i+1] != 'u' {
			continue
		}
		// A backslash that is itself escaped starts no escape: the text
		// backslash-backslash-u-0000 decodes to a literal backslash
		// followed by "u0000", which Postgres stores happily. Only an odd
		// run of backslashes ending here introduces one.
		backslashes := 0
		for j := i; j >= 0 && raw[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			continue
		}
		if raw[i+2] == '0' && raw[i+3] == '0' && raw[i+4] == '0' && raw[i+5] == '0' {
			return true
		}
	}
	return false
}

// UsableSigningKeys reports how many keys in a JWK Set document could ever
// verify an assertion: parseable, public, usable for signing, and on the
// algorithm allowlist. Registration surfaces require at least one for a
// private_key_jwt client, since a set the resolver would parse to nothing
// registers a client that can never authenticate.
func UsableSigningKeys(raw json.RawMessage) (int, error) {
	set, err := parseKeySet(raw)
	if err != nil {
		return 0, err
	}
	return len(set.Keys), nil
}

// privateKeyMembers are the JWK members that only appear on a private key:
// "d" (RSA / EC / OKP private component) plus the optional RFC 7518 §6.3.2
// RSA CRT parameters and additional-primes member. The CRT members matter
// even though "d" accompanies them in a well-formed private key: a malformed
// key carrying only "p"/"q" would fail go-jose's parser and be tolerated out
// of the set, which must not let the private material it carries slip past
// the fatal screen.
var privateKeyMembers = []string{"d", "p", "q", "dp", "dq", "qi", "oth"}

// screenKeyMaterial is the per-key half of the public-only rule, shared
// between ValidatePublicOnly's whole-document walk and parseKeySet's
// single-pass parse so the two can never screen differently.
func screenKeyMaterial(key map[string]json.RawMessage) error {
	for _, member := range privateKeyMembers {
		if _, ok := key[member]; ok {
			return ErrPrivateKeyMaterial
		}
	}
	if _, ok := key["k"]; ok {
		return ErrSymmetricKeyMaterial
	}
	return nil
}

// parseKeySet parses a JWK Set document into the usable verification keys it
// contains. The set-level screens are fatal (private or symmetric material
// rejects the whole document); individual keys are tolerated out instead:
// keys that fail go-jose's parser (unsupported kty, malformed parameters),
// declare a use other than "sig", or declare an algorithm outside the
// allowlist are skipped rather than failing the set. Real key sets routinely
// carry encryption keys and exotic entries alongside their signing keys, and
// rejecting the document for those would be an interop bug — a skipped key
// simply can never be selected.
//
// A key that declares no alg is kept: RFC 7517 makes the member optional and
// omission is common. The allowlist can only reject a declared none / HS*,
// never require a declaration; the algorithm actually used to verify an
// assertion is constrained separately by the caller's parse allowlist.
func parseKeySet(raw json.RawMessage) (jose.JSONWebKeySet, error) {
	if len(raw) == 0 {
		return jose.JSONWebKeySet{Keys: nil}, fmt.Errorf("empty key set document: %w", ErrKeySetInvalid)
	}

	// One pass over the document: the envelope is unmarshalled once and each
	// key is screened from its raw members before go-jose sees it, rather
	// than running ValidatePublicOnly's separate whole-document walk first.
	// Per-key screening happens before the jose parse so a malformed key
	// cannot smuggle material past a stricter parser by failing it.
	var envelope struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return jose.JSONWebKeySet{Keys: nil}, fmt.Errorf("parse key set envelope: %w", ErrKeySetInvalid)
	}
	// RFC 7517 §5 makes "keys" the set's one required member, so "null",
	// "{}", and {"keys":null} are malformed documents, not empty sets; only
	// an actual (possibly empty) array is accepted.
	if envelope.Keys == nil {
		return jose.JSONWebKeySet{Keys: nil}, fmt.Errorf(`key set missing "keys" array: %w`, ErrKeySetInvalid)
	}

	keys := make([]jose.JSONWebKey, 0, len(envelope.Keys))
	for _, rawKey := range envelope.Keys {
		var members map[string]json.RawMessage
		if err := json.Unmarshal(rawKey, &members); err != nil {
			return jose.JSONWebKeySet{Keys: nil}, fmt.Errorf("parse key entry: %w", ErrKeySetInvalid)
		}
		if err := screenKeyMaterial(members); err != nil {
			return jose.JSONWebKeySet{Keys: nil}, err
		}

		var key jose.JSONWebKey
		if err := key.UnmarshalJSON(rawKey); err != nil {
			continue
		}
		if !key.Valid() || !key.IsPublic() {
			continue
		}
		if key.Use != "" && key.Use != "sig" {
			continue
		}
		if key.Algorithm != "" && !isAllowedSignatureAlgorithm(key.Algorithm) {
			continue
		}
		keys = append(keys, key)
	}
	return jose.JSONWebKeySet{Keys: keys}, nil
}

// selectKey picks the verification key for kid out of an already-screened
// set. With a kid, the first key whose key ID matches wins. Without one —
// RFC 7515 makes the header optional — the set must contain exactly one
// usable key for the choice to be unambiguous; guessing among several would
// mean trying keys until a signature verifies, which turns verification into
// an oracle.
func selectKey(set jose.JSONWebKeySet, kid string) (*jose.JSONWebKey, error) {
	if kid == "" {
		if len(set.Keys) == 1 {
			key := set.Keys[0]
			return &key, nil
		}
		return nil, fmt.Errorf("assertion carries no kid and the key set has %d usable keys: %w", len(set.Keys), ErrKeyNotFound)
	}
	for _, key := range set.Keys {
		if key.KeyID == kid {
			selected := key
			return &selected, nil
		}
	}
	return nil, fmt.Errorf("kid %q: %w", kid, ErrKeyNotFound)
}
