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

// VerificationKeyForAlgorithm uses a stored key after a transient fetch error
// only when it is still fresh or inside the bounded stale window and matches
// the assertion's kid and algorithm. A newer successful refresh replaces the
// set, so an older in-process copy cannot resurrect a removed key.
func (k *IssuerVerificationKeys) VerificationKeyForAlgorithm(ctx context.Context, source jwks.Source, kid string, algorithm jose.SignatureAlgorithm) (*jose.JSONWebKey, error) {
	key, err := k.keys.VerificationKeyForAlgorithm(ctx, source, kid, algorithm)
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
	if len(state.Document) == 0 || state.ExpiresAt.IsZero() || !now.Before(state.ExpiresAt.Add(MaxStaleKeys)) {
		return nil, fmt.Errorf("resolve issuer verification key: %w", err)
	}
	stale, staleErr := jwks.VerificationKeyFromDocument(state.Document, kid, algorithm)
	if staleErr != nil {
		return nil, fmt.Errorf("resolve issuer verification key: %w", err)
	}
	return stale, nil
}
