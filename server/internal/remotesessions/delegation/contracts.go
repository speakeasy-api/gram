// Package delegation owns retained credential state and its refresh CAS protocol.
// OIDC discovery, signature verification and one-shot login envelopes remain in
// remotesessions and enter through these narrow, internal adapters.
package delegation

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Provider exposes only binding and policy facts, never registration secrets.
// Implementations must represent the currently authorized stored registration.
type Provider interface {
	Binding(humanID string) Binding
	IssuerURL() string
	DelegationConfigurationHash() string
	OfflineConfigurationHash() string
	OfflineRequested() bool
	OfflineSupported() bool
}

// Credentials is the existing explicit secret handoff, not a serializable model.
// Implementations must redact formatting and must not be logged by consumers.
type Credentials interface {
	IDToken() string
	RefreshToken() string
	RefreshExpiresAt() *time.Time
	RefreshExpiresIn() int64
	ReceivedAt() time.Time
}

// Identity must be verified before admission. WithCredentials must consume the
// request-local envelope at most once, including when its consumer fails.
type Identity interface {
	IssuerURL() string
	SubjectID() string
	NonceValue() string
	Expiration() time.Time
	WithCredentials(func(Credentials) error) error
	DiscardCredentials()
}

// RefreshResult may accompany a verification error. In that case Credentials
// are for encrypted quarantine only; Identity must not be admitted or replayed.
type RefreshResult struct {
	Identity    Identity
	Credentials Credentials
}

func (r RefreshResult) String() string               { return "[delegation refresh result]" }
func (r RefreshResult) GoString() string             { return r.String() }
func (r RefreshResult) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

// Dependencies connect the state machine to live authorization configuration
// and the provider request path. Now defaults to time.Now.
type Dependencies struct {
	Now             func() time.Time
	LoadBinding     func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error)
	LoadProvider    func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error)
	RefreshIdentity func(context.Context, Provider, string, string, string) (*RefreshResult, error)
}

type RefreshFailure string

const (
	RefreshAmbiguous       RefreshFailure = "ambiguous"
	RefreshInvalidGrant    RefreshFailure = "invalid_grant"
	RefreshConfiguration   RefreshFailure = "configuration"
	RefreshInvalidIdentity RefreshFailure = "invalid_identity"
	RefreshRetryable       RefreshFailure = "retryable"
)

// RefreshError carries a classified outcome only, never upstream payloads.
type RefreshError struct{ Kind RefreshFailure }

func (e *RefreshError) Error() string { return "delegation refresh failed" }
