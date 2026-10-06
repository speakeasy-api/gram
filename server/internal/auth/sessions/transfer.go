package sessions

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// TransferCodeTTL is how long a session transfer code stays redeemable.
const TransferCodeTTL = 60 * time.Second

// ErrTransferCodeNotFound reports a transfer code that is unknown, expired, or
// already redeemed. Any other cache failure is returned wrapped as is, so a
// caller can tell an outage from a code that is simply gone.
var ErrTransferCodeNotFound = errors.New("transfer code is unknown, expired, or already used")

// ErrTransferWrongHost reports a transfer code issued for another host. The
// code stays redeemable on its own host.
var ErrTransferWrongHost = errors.New("transfer code was issued for another host")

// ErrSessionNotTransferable reports an impersonation or support session. Those
// sessions stay on the host where they were created.
var ErrSessionNotTransferable = errors.New("impersonation and support sessions cannot be transferred")

// TransferRecord is the server-side state behind a transfer code. It has no
// session ID on purpose: the destination host mints a new session.
type TransferRecord struct {
	UserID               string
	ActiveOrganizationID string
	// WorkOSSessionID lets logout on the destination host revoke the
	// WorkOS session.
	WorkOSSessionID string
	SourceHost      string
	TargetHost      string
	// NonceHash is the SHA-256 of the browser binding nonce that transferIn's
	// start mode set as a cookie on the target host. It also names that
	// cookie. Only the browser holding the cookie can redeem the code.
	NonceHash string
}

// TransferManager issues and redeems one-time session transfer codes. The
// code is an opaque random value; the record it points to lives in the cache.
type TransferManager struct {
	cache cache.Cache
}

// NewTransferManager creates a transfer manager backed by c.
func NewTransferManager(c cache.Cache) *TransferManager {
	return &TransferManager{cache: c}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// TransferNonceHash is the hash a transfer record keeps of its browser
// binding nonce. The nonce itself is never stored.
func TransferNonceHash(nonce string) string {
	return sha256Hex(nonce)
}

// BoundTo reports, in constant time, whether nonce is the browser binding the
// record was issued for.
func (r TransferRecord) BoundTo(nonce string) bool {
	return nonce != "" && subtle.ConstantTimeCompare([]byte(sha256Hex(nonce)), []byte(r.NonceHash)) == 1
}

// cacheError tells a missing record (ErrTransferCodeNotFound) from a cache
// failure.
func cacheError(op string, err error) error {
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return ErrTransferCodeNotFound
	}
	return fmt.Errorf("%s transfer record: %w", op, err)
}

// transferKey derives the cache key from a code. Only a hash of the code is
// stored, so a cache dump cannot be replayed as a transfer code.
func transferKey(code string) string {
	return "session_transfer:" + sha256Hex(code)
}

// Create stores a transfer record for session, bound to the browser that
// holds nonce, and returns its one-time code.
func (m *TransferManager) Create(ctx context.Context, session Session, nonce, sourceHost, targetHost string) (string, error) {
	if nonce == "" {
		return "", errors.New("transfer nonce is required")
	}
	if session.ImpersonatorEmail != "" || session.SupportOrganizationID != "" {
		return "", ErrSessionNotTransferable
	}

	// A transfer code is a bearer credential like a session token, so it uses
	// the same 256-bit opaque generator.
	code, err := NewSessionID()
	if err != nil {
		return "", fmt.Errorf("generate transfer code: %w", err)
	}

	record := TransferRecord{
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		WorkOSSessionID:      session.WorkOSSessionID,
		SourceHost:           sourceHost,
		TargetHost:           targetHost,
		NonceHash:            sha256Hex(nonce),
	}
	if err := m.cache.Set(ctx, transferKey(code), record, TransferCodeTTL); err != nil {
		return "", fmt.Errorf("store transfer record: %w", err)
	}

	return code, nil
}

// Lookup returns the record for code when it was issued for targetHost:
// ErrTransferCodeNotFound when there is none, ErrTransferWrongHost when it was
// issued for another host, and a wrapped error when the cache fails. It
// does not consume the code, so a caller can check the browser binding with
// BoundTo and run further checks first, and call Consume only once they pass.
func (m *TransferManager) Lookup(ctx context.Context, code, targetHost string) (TransferRecord, error) {
	var record TransferRecord
	if err := m.cache.Get(ctx, transferKey(code), &record); err != nil {
		return TransferRecord{}, cacheError("look up", err)
	}
	if record.TargetHost != targetHost {
		return TransferRecord{}, ErrTransferWrongHost
	}
	return record, nil
}

// Consume atomically deletes the record for code. Of two concurrent callers,
// only one succeeds; the other gets ErrTransferCodeNotFound. A cache failure
// is returned wrapped.
func (m *TransferManager) Consume(ctx context.Context, code string) error {
	// GetAndDelete needs a destination; the record itself is not used.
	var record TransferRecord
	if err := m.cache.GetAndDelete(ctx, transferKey(code), &record); err != nil {
		return cacheError("consume", err)
	}
	return nil
}
