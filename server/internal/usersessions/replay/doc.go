// Package replay enforces single use of assertion identifiers.
//
// An assertion carrying a jti is a bearer credential for as long as it stays
// within its validity window: whoever intercepts one can present it again
// until it expires. RFC 7523 §3 makes the jti the mechanism that closes that
// window, and closing it requires the server to remember which identifiers it
// has already honoured. That memory is what this package is.
//
// # One store, several consumers
//
// Client assertions (private_key_jwt) and third-party issuer assertions carry
// different claims, resolve their keys differently, and authenticate different
// parties, but the replay question is identical for both: has this exact
// identifier, from this exact party, been seen before? So the store lives here
// once and each consumer supplies its own Key composition and hold window.
//
// This is a sibling keyspace to the revoked-access-token cache, not the same
// one: different lifetime, different scope, different subject.
//
// # Fail closed
//
// Reserve reports an error rather than a verdict when the store cannot answer,
// and callers must treat that as a rejection. A replay guard that answered
// "not seen before" whenever its backing store was unreachable would delete
// the property it exists to provide precisely when something is already wrong,
// which is why NewRedisGuard refuses a nil client instead of degrading to a
// cache whose no-op implementation reports every caller as the first.
package replay
