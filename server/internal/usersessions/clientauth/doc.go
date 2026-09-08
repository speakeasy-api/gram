// Package clientauth verifies RFC 7523 §2.2 client assertions, the evidence
// a private_key_jwt client presents in place of a shared secret.
//
// It owns the decision and none of the storage: callers hand it the client's
// key source, the audiences their endpoint accepts, and the identity the
// assertion has to match, and it answers whether the client authenticated.
// Key resolution belongs to the jwks package and replay memory to the replay
// package, both injected, so this package is exercised without a database and
// its rules are readable in one place.
//
// # What makes an assertion acceptable
//
// The client_assertion_type must be RFC 7523's jwt-bearer URN. The assertion
// must be a compact JWS signed with an asymmetric algorithm, verify under a
// key the client's own published key set names, carry iss and sub both equal
// to the client_id being authenticated, name an audience this endpoint
// accepts, sit inside its temporal claims, and carry a jti not spent before.
//
// # Algorithm confusion is closed at parse time
//
// The permitted algorithms are fixed before the token is read, so `none` and
// every HS* are rejected as malformed rather than reaching verification. This
// matters because a client's verification key is public: an assertion signed
// HS256 using that public key as the shared secret is the canonical attack,
// and making the allowlist a property of parsing means no later check has to
// remember to prevent it.
//
// # Signature before claims
//
// Verify examines no claim until the signature verifies. This costs a key
// resolution on assertions that a cheap claim comparison could have refused,
// and it is deliberate: it means every claim-shaped rejection in the logs was
// produced by the holder of the client's private key. The audience label in
// particular exists to learn what real clients send, and that signal is only
// worth having if forged assertions cannot contribute to it.
//
// UnverifiedClientID is the one deliberate exception. It reads iss and sub
// without checking the signature so that a request which omitted client_id
// can still select a row, and that value authenticates nothing: Verify then
// checks the same claims against the row's own key set.
//
// # Failures are deliberately indistinguishable to the client
//
// Every rejection carries a Reason for logs and metrics, and callers are
// expected to answer all of them with one invalid_client, so that nothing
// about an assertion's verification (unknown key, bad signature, spent
// identifier) is learnable from the wire. A client_id is a URL for CIMD
// clients, so a response that separated "no such client" from "bad
// signature" would tell an attacker which vendors' clients an issuer has
// seen; callers are expected to use the same description for the lookup
// that runs before this package too.
package clientauth
