package clientauth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ReplayID names how a profile derives the identifier the replay guard
// reserves.
//
// A client assertion is minted by software we can hold to RFC 7523 §3, so
// requiring a jti is reasonable: if one is missing that is the client's bug
// and it can fix it. A workload assertion is minted by a platform that made
// its own choices and will not revisit them for us, and two of the four
// platforms surveyed do not present a jti at all — Google emits no replay
// identifier, Microsoft Entra calls it uti. Requiring one rejects both before
// any admission logic runs.
//
// RFC 7523 §3 says a JWT MAY carry a jti. Requiring one is our choice, and
// the protection it buys can be obtained without it.
type ReplayID uint8

const (
	// ReplayIDJTI requires a jti and reserves exactly that value.
	//
	// The zero value on purpose, so every existing client-assertion call
	// site keeps RFC 7523 §3 behaviour without naming a strategy.
	ReplayIDJTI ReplayID = iota

	// ReplayIDDerived resolves an identifier in order: jti, then uti, then
	// a digest of the assertion itself.
	//
	// The ladder is fixed in code rather than configured per issuer,
	// deliberately. Entra is the only platform identified that spells the
	// claim differently, and a configured claim name would mean a column on
	// the issuer table plus the migration and management API to set it —
	// for one vendor's choice of word. Promoting the ladder to
	// configuration later is additive and costs nothing now.
	ReplayIDDerived
)

// replayIDClaims carries the claim names the ladder consults beyond the
// registered set jwt.Claims already decodes.
type replayIDClaims struct {
	// UTI is Entra's spelling. Its own documentation defines it as
	// "equivalent to jti in the JWT specification. Unique, per-token
	// identifier that is case-sensitive."
	UTI optionalString `json:"uti"`
}

// optionalString is a claim read as a string when it is one and treated as
// absent when it is anything else.
//
// Every profile decodes the payload into this set, including the client one
// that never consults uti. A plain string field would make an unrelated
// party's non-string uti fail the whole decode, rejecting an otherwise valid
// assertion as signature-invalid. A claim that cannot be read is no
// identifier at all, so the ladder falls to the next rung instead.
type optionalString string

func (o *optionalString) UnmarshalJSON(data []byte) error {
	var value string
	if json.Unmarshal(data, &value) == nil {
		*o = optionalString(value)
	}

	return nil
}

// resolveReplayID returns the identifier to reserve and whether that
// identifier distinguishes this token from every other one the same party
// mints.
//
// The second return is the whole reason this function exists. A repeated jti
// is a *different* token reusing an identifier, which is a replay and must be
// refused. A repeated digest is the same bytes arriving twice, which some
// platforms do legitimately: Google's metadata server serves one cached token
// for most of its validity window, so an agent that re-reads its own identity
// gets byte-identical output rather than a fresh assertion. Refusing that
// would break the caller rather than an attacker.
//
// So the digest is bounded reuse, not single use. It still costs an attacker
// everything that matters — a captured token remains unusable past its own
// exp, and unusable at all without the admission this runs before.
func resolveReplayID(strategy ReplayID, jti string, extra replayIDClaims, assertion string) (id string, exact bool, ok bool) {
	if jti != "" {
		return jti, true, true
	}
	// Anything but the derived strategy requires a jti, so a value this
	// package does not recognise fails closed on the strictest rung rather
	// than falling through to the most permissive one.
	if strategy != ReplayIDDerived {
		return "", false, false
	}
	if extra.UTI != "" {
		return string(extra.UTI), true, true
	}

	return assertionDigest(assertion), false, true
}

// assertionDigest identifies an assertion by its own bytes.
//
// The prefix is not decoration: it keeps a derived identifier from ever
// colliding with a platform-minted one that happens to be 64 hex characters,
// which costs nothing to rule out and would be unpleasant to diagnose.
//
// The digest covers the whole compact serialization — header, payload and
// signature — so two tokens differing anywhere, including in a signature over
// identical claims, are different identifiers.
func assertionDigest(assertion string) string {
	sum := sha256.Sum256([]byte(assertion))

	return "sha256:" + hex.EncodeToString(sum[:])
}
