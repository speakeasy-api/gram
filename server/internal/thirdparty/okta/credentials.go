package okta

import "context"

// CredentialProvider reads a client_secret_basic credential's live state so a
// client never exchanges a withdrawn secret and never presents a Bearer token
// once the connection is pinned to DPoP. Implementations must be comparable
// because Config is a factory cache key.
type CredentialProvider interface {
	// Acquire locks the credential for one token exchange, so a withdrawal
	// cannot commit while the exchange is in flight, and returns its current
	// secret and pin. A provider bound to a caller's transaction reads through
	// that transaction, including its uncommitted replacement secret.
	Acquire(context.Context, Config) (CredentialLease, error)

	// RequireDPoP reports the durable pin without taking a lease. The client
	// consults it before reusing a cached Bearer token, so a pin recorded by
	// another process (a verification, another worker) stops the Bearer reuse
	// before the token expires.
	RequireDPoP(context.Context, Config) (bool, error)
}

// CredentialLease is one token exchange's view of the credential. The client
// calls Observe with the minted token's binding before it uses the token for
// any read, and discards the token when Observe fails, so a token is only ever
// presented after its binding was recorded. Close releases the lease on every
// path.
//
// A lease that owns its transaction commits the observation in Observe, so it
// is durable before the token is used. A lease bound to the caller's
// transaction records the pin in that transaction: the caller makes it durable
// by committing, or by re-pinning after a rollback, before it acts on the
// result. A crash between Observe and that commit loses the observation, which
// is accepted because the caller must not take a second pool connection while
// it holds the connection lock.
type CredentialLease interface {
	// EncryptedSecret is the client secret ciphertext current under the lease.
	EncryptedSecret() string

	// RequireDPoP reports whether the connection is pinned to DPoP-bound tokens.
	RequireDPoP() bool

	// Observe records whether the minted token was DPoP-bound. Recording is
	// monotonic: a Bearer observation never clears an earlier pin.
	Observe(context.Context, bool) error

	// Close releases the lease; it is safe to call after Observe.
	Close()
}
