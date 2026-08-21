package jwks

import (
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
// on non-public keys — "d" (RSA / EC / OKP private component) and "k"
// (symmetric oct key) — over the raw JSON, so a malformed key cannot smuggle
// material past a stricter parser by failing it. A nil document is accepted:
// absence of a key set is the caller's policy question, not a validation
// failure.
//
// Shared with the cimd package, whose -02 §4.1 document screening is the same
// rule with its own wire-format wrapping.
func ValidatePublicOnly(raw json.RawMessage) error {
	if raw == nil {
		return nil
	}

	var keySet struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &keySet); err != nil {
		return fmt.Errorf("parse key set: %w", ErrKeySetInvalid)
	}
	for _, key := range keySet.Keys {
		if err := screenKeyMaterial(key); err != nil {
			return err
		}
	}
	return nil
}

// screenKeyMaterial is the per-key half of the public-only rule, shared
// between ValidatePublicOnly's whole-document walk and parseKeySet's
// single-pass parse so the two can never screen differently.
func screenKeyMaterial(key map[string]json.RawMessage) error {
	if _, ok := key["d"]; ok {
		return ErrPrivateKeyMaterial
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
