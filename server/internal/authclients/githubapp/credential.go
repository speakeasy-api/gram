package githubapp

import (
	"context"
	"log/slog"
	"time"
)

// ClientCredential holds renewable GitHub bearer material without depending on
// session management. Only Value exposes the secret.
type ClientCredential struct {
	value     string
	expiresAt time.Time
	forget    func(context.Context) (bool, error)
}

func newClientCredential(value string, expiresAt time.Time, forget func(context.Context) (bool, error)) ClientCredential {
	return ClientCredential{value: value, expiresAt: expiresAt, forget: forget}
}

func (c ClientCredential) Value() string        { return c.value }
func (c ClientCredential) Scheme() string       { return "Bearer" }
func (c ClientCredential) ExpiresAt() time.Time { return c.expiresAt }

func (c ClientCredential) Forget(ctx context.Context) (bool, error) {
	if c.forget == nil {
		return false, nil
	}
	return c.forget(ctx)
}

func (c ClientCredential) String() string               { return "[redacted client credential]" }
func (c ClientCredential) GoString() string             { return c.String() }
func (c ClientCredential) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (c ClientCredential) LogValue() slog.Value         { return slog.StringValue(c.String()) }
