package supporthandoff

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

const GrantTTL = time.Minute

const tokenBytes = 32

// Grant is the short-lived authorization for a dashboard support login.
// It deliberately contains an organization ID, never a redirect or slug.
type Grant struct {
	OrganizationID string
}

// Store persists and atomically consumes one-time support handoffs.
type Store struct {
	cache cache.Cache
}

func NewStore(c cache.Cache) *Store {
	return &Store{cache: c}
}

func (s *Store) put(ctx context.Context, token string, grant Grant) error {
	if err := s.cache.Set(ctx, key(token), grant, GrantTTL); err != nil {
		return fmt.Errorf("store support handoff: %w", err)
	}
	return nil
}

// Consume atomically redeems a handoff. A token can succeed at most once.
func (s *Store) Consume(ctx context.Context, token string) (Grant, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != tokenBytes {
		return Grant{}, fmt.Errorf("consume support handoff: invalid token")
	}

	var grant Grant
	if err := s.cache.GetAndDelete(ctx, key(token), &grant); err != nil {
		return Grant{}, fmt.Errorf("consume support handoff: %w", err)
	}
	if grant.OrganizationID == "" {
		return Grant{}, fmt.Errorf("consume support handoff: missing organization id")
	}
	return grant, nil
}

// Issuer creates cryptographically random opaque handoffs. It is shared so the
// admin API can issue the exact capability auth.login consumes.
type Issuer struct {
	store *Store
}

func NewIssuer(store *Store) *Issuer {
	return &Issuer{store: store}
}

func (i *Issuer) Issue(ctx context.Context, organizationID string) (string, error) {
	if organizationID == "" {
		return "", fmt.Errorf("issue support handoff: missing organization id")
	}
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("issue support handoff token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	if err := i.store.put(ctx, token, Grant{OrganizationID: organizationID}); err != nil {
		return "", err
	}
	return token, nil
}

func key(token string) string {
	digest := sha256.Sum256([]byte(token))
	return "auth:support_handoff:" + hex.EncodeToString(digest[:])
}
