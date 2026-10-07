package clientcredentials

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Source obtains the upstream credential a remote session client holds for
// itself. The client credentials grant is one source; a stored credential,
// such as an API key, can be another.
type Source interface {
	// Credential returns a usable upstream credential for the request.
	Credential(ctx context.Context, req Request) (Credential, error)

	// Forget discards cred after the upstream rejected it, and reports whether
	// a later Credential call can return a different credential.
	Forget(ctx context.Context, cred Credential) (bool, error)
}

// Request names the client whose upstream credential is wanted.
type Request struct {
	// OrganizationID is the tenant the client must belong to.
	OrganizationID string

	// ClientID is the remote_session_clients row.
	ClientID uuid.UUID

	// Resource is the RFC 8707 resource indicator, sent verbatim. Empty sends
	// none.
	Resource string
}

// Scheme is how a credential is presented to the upstream.
type Scheme string

const (
	// SchemeBearer presents the credential as an RFC 6750 bearer token in the
	// Authorization header.
	SchemeBearer Scheme = "Bearer"
)

// Credential is an upstream credential. It formats redacted; only Value
// exposes the secret.
type Credential struct {
	// value is the secret presented upstream.
	value string

	// scheme is how value is presented upstream.
	scheme Scheme

	// expiresAt is when the credential stops being served.
	expiresAt time.Time

	// mintedAt is when the credential was requested, which gates Forget.
	mintedAt time.Time

	// entry is the cached entry the credential came from, so Forget can drop
	// exactly that entry. It is nil when the credential was never cached.
	entry *credentialEntry
}

// NewCredential builds a credential for a Source other than the client
// credentials grant, such as a stored API key. Forget on Minter does not apply
// to it.
func NewCredential(value string, scheme Scheme, expiresAt time.Time) Credential {
	return Credential{value: value, scheme: scheme, expiresAt: expiresAt, mintedAt: time.Time{}, entry: nil}
}

// Value is the secret presented to the upstream.
func (c Credential) Value() string { return c.value }

// Scheme is how Value is presented to the upstream.
func (c Credential) Scheme() Scheme { return c.scheme }

// ExpiresAt is when the credential stops being served, ahead of its upstream
// expiry so a proxied call never starts with a credential that expires in
// flight.
func (c Credential) ExpiresAt() time.Time { return c.expiresAt }

func (c Credential) String() string               { return "[redacted client credential]" }
func (c Credential) GoString() string             { return c.String() }
func (c Credential) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (c Credential) LogValue() slog.Value         { return slog.StringValue(c.String()) }
