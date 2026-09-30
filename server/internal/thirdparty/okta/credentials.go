package okta

import "context"

// CredentialProvider acquires current credential state and serializes an entire
// exchange with credential withdrawal. Verification may use its existing transaction.
type CredentialProvider interface {
	Acquire(context.Context, Config) (CredentialLease, error)
}

// CredentialLease must be closed on every path. Observe durably records binding
// before a token is used; transaction-bound leases leave commit to their caller.
type CredentialLease interface {
	EncryptedSecret() string
	RequireDPoP() bool
	Observe(context.Context, bool) error
	Close()
}
