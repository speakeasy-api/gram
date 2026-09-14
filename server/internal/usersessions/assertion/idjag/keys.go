package idjag

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"

	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// MaxStaleKeys bounds use of a previously verified key set after its normal
// cache freshness expires and a transient upstream refresh fails.
const MaxStaleKeys = time.Hour

// IssuerVerificationKeys applies the ID-JAG stale-key policy to the shared
// JWKS resolver. The cache and resolver must use the same storage policy.
type IssuerVerificationKeys struct {
	keys  assertioncore.VerificationKeys
	cache jwks.Cache
}

// NewIssuerVerificationKeys binds a rate-limited key resolver to its durable
// trusted-issuer cache.
func NewIssuerVerificationKeys(keys assertioncore.VerificationKeys, cache jwks.Cache) (*IssuerVerificationKeys, error) {
	if keys == nil || cache == nil {
		return nil, errors.New("idjag: key resolver and cache are required")
	}
	return &IssuerVerificationKeys{keys: keys, cache: cache}, nil
}

var _ assertioncore.VerificationKeys = (*IssuerVerificationKeys)(nil)

// VerificationKey uses a stale stored key only for a transient fetch error,
// within a fixed window, and when the assertion names a key already present
// in the currently stored set. Successful refreshes replace that set, so a
// removed key cannot be resurrected by an older in-process copy.
func (k *IssuerVerificationKeys) VerificationKey(ctx context.Context, source jwks.Source, kid string) (*jose.JSONWebKey, error) {
	key, err := k.keys.VerificationKey(ctx, source, kid)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, jwks.ErrKeySetUnavailable) || kid == "" {
		return nil, fmt.Errorf("resolve issuer verification key: %w", err)
	}
	state, cacheErr := k.cache.Get(ctx, source.CacheKey())
	if cacheErr != nil {
		return nil, fmt.Errorf("read stale issuer keys: %w", cacheErr)
	}
	now := time.Now()
	if len(state.Document) == 0 || state.ExpiresAt.IsZero() || now.Before(state.ExpiresAt) || !now.Before(state.ExpiresAt.Add(MaxStaleKeys)) {
		return nil, fmt.Errorf("resolve issuer verification key: %w", err)
	}
	stale, staleErr := jwks.VerificationKeyFromDocument(state.Document, kid)
	if staleErr != nil {
		return nil, fmt.Errorf("resolve issuer verification key: %w", err)
	}
	return stale, nil
}
