package remotesessions

import (
	"errors"
	"log/slog"
	"time"
)

// EphemeralFederatedCredentials is available only through the explicit,
// post-authorization handoff. Getters intentionally return raw secrets: a
// consumer must not log them and owns any separately authorized retention.
// This is a single-owner, synchronous handoff; do not share it across goroutines.
// Formatting and serialization of the envelope itself are always redacted.
type EphemeralFederatedCredentials struct {
	idToken          string
	refreshToken     string
	expiresIn        int
	refreshExpiresIn int64
	receivedAt       time.Time
}

func (c EphemeralFederatedCredentials) IDToken() string              { return c.idToken }
func (c EphemeralFederatedCredentials) RefreshToken() string         { return c.refreshToken }
func (c EphemeralFederatedCredentials) ExpiresIn() int               { return c.expiresIn }
func (c EphemeralFederatedCredentials) RefreshExpiresIn() int64      { return c.refreshExpiresIn }
func (c EphemeralFederatedCredentials) ReceivedAt() time.Time        { return c.receivedAt }
func (c EphemeralFederatedCredentials) String() string               { return "[redacted federated credentials]" }
func (c EphemeralFederatedCredentials) GoString() string             { return c.String() }
func (c EphemeralFederatedCredentials) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (c EphemeralFederatedCredentials) LogValue() slog.Value         { return slog.StringValue(c.String()) }

// WithCredentials consumes the envelope at most once, even if the consumer
// fails. Call only after provisioned-human resolution and organization access.
// The identity and consumer are single-owner and synchronous, not concurrency-safe.
// A nil consumer discards credentials; login never requires a consumer.
func (i *FederatedIdentity) WithCredentials(consume func(EphemeralFederatedCredentials) error) error {
	if i == nil || i.credentials == nil || i.credentials.idToken == "" {
		return errors.New("federated credentials unavailable")
	}
	credentials := *i.credentials
	i.DiscardCredentials()
	if consume == nil {
		return nil
	}
	return consume(credentials)
}

// DiscardCredentials releases the request-local envelope. Callers should defer
// this immediately after a successful exchange, including on access denial.
// This releases references only: it does not zero immutable strings or revoke
// tokens, and cannot erase copies already retained by a consumer.
func (i *FederatedIdentity) DiscardCredentials() {
	if i == nil || i.credentials == nil {
		return
	}
	*i.credentials = EphemeralFederatedCredentials{idToken: "", refreshToken: "", expiresIn: 0, refreshExpiresIn: 0, receivedAt: time.Time{}}
	i.credentials = nil
}

func (i FederatedIdentity) String() string       { return "[verified federated identity]" }
func (i FederatedIdentity) GoString() string     { return i.String() }
func (i FederatedIdentity) LogValue() slog.Value { return slog.StringValue(i.String()) }
