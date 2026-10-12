package identitychaining

import (
	"log/slog"
	"time"
)

// Token is a downstream access token obtained by identity chaining. It formats
// redacted; only Value exposes the credential.
type Token struct {
	value     string
	expiresAt time.Time
}

// Value is the bearer token for the upstream request.
func (t Token) Value() string { return t.value }

// ExpiresAt is when the token stops being reused.
func (t Token) ExpiresAt() time.Time { return t.expiresAt }

func (t Token) String() string               { return "[redacted chained token]" }
func (t Token) GoString() string             { return t.String() }
func (t Token) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }
func (t Token) LogValue() slog.Value         { return slog.StringValue(t.String()) }
