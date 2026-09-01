package netingress

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultTokenAudience    = "gram-netingress" //nolint:gosec // public TokenReview audience, not a credential
	maxAttestationBytes     = 16 * 1024
	maxAttestationCacheSize = 4096
	negativeCacheTTL        = 2 * time.Second
	servingStateRecheckTTL  = 2 * time.Second
	maxSourceLimiters       = 4096
	globalReviewRate        = rate.Limit(500)
	globalReviewBurst       = 1000
)

var ErrAttestationRejected = errors.New("private ingress attestation rejected")

type TokenReviewer interface {
	Create(context.Context, *authenticationv1.TokenReview, metav1.CreateOptions) (*authenticationv1.TokenReview, error)
}

type AttestorIngressLookup interface {
	ByAttestor(context.Context, string, string) (Ingress, error)
	Recheck(context.Context, Ingress) error
}

type cacheEntry struct {
	ingress     Ingress
	expiresAt   time.Time
	rejected    bool
	lastChecked time.Time
}

type AttestationVerifier struct {
	reviewer TokenReviewer
	lookup   AttestorIngressLookup
	audience string
	maxTTL   time.Duration
	now      func() time.Time

	mu             sync.Mutex
	cache          map[[32]byte]cacheEntry
	sourceLimiters map[string]*rate.Limiter
	globalLimiter  *rate.Limiter
	rechecks       singleflight.Group
}

func NewAttestationVerifier(reviewer TokenReviewer, lookup AttestorIngressLookup, audience string, maxTTL time.Duration) *AttestationVerifier {
	if audience == "" {
		audience = DefaultTokenAudience
	}
	if maxTTL <= 0 {
		maxTTL = 30 * time.Second
	}
	return &AttestationVerifier{
		reviewer:       reviewer,
		lookup:         lookup,
		audience:       audience,
		maxTTL:         maxTTL,
		now:            time.Now,
		mu:             sync.Mutex{},
		cache:          make(map[[32]byte]cacheEntry),
		sourceLimiters: make(map[string]*rate.Limiter),
		globalLimiter:  rate.NewLimiter(globalReviewRate, globalReviewBurst),
		rechecks:       singleflight.Group{},
	}
}

func (v *AttestationVerifier) Verify(ctx context.Context, token, source string) (Ingress, error) {
	if v.reviewer == nil || v.lookup == nil {
		return Ingress{}, fmt.Errorf("%w: verifier is not configured", ErrAttestationRejected)
	}
	if token == "" || strings.TrimSpace(token) != token || len(token) > maxAttestationBytes || source == "" {
		return Ingress{}, fmt.Errorf("%w: invalid bearer token", ErrAttestationRejected)
	}

	hash := sha256.Sum256([]byte(token))
	now := v.now()
	if cached, ok := v.cached(hash, now); ok {
		if cached.rejected {
			return Ingress{}, fmt.Errorf("%w: token was recently rejected", ErrAttestationRejected)
		}
		if now.Sub(cached.lastChecked) >= servingStateRecheckTTL {
			return v.recheckCached(ctx, hash)
		}
		return cached.ingress, nil
	}

	if !v.allowSource(source) {
		return Ingress{}, errors.New("token review rate limit exceeded")
	}
	//nolint:exhaustruct // request populates only TokenReview spec; API server owns response metadata/status
	review, err := v.reviewer.Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{
			Token:     token,
			Audiences: []string{v.audience},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return Ingress{}, fmt.Errorf("review private ingress token: %w", err)
	}
	if review == nil || !review.Status.Authenticated || review.Status.Error != "" || !containsAudience(review.Status.Audiences, v.audience) {
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: token review denied", ErrAttestationRejected)
	}

	namespace, serviceAccount, err := parseServiceAccountSubject(review.Status.User.Username)
	if err != nil {
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: %w", ErrAttestationRejected, err)
	}
	expiresAt, err := tokenExpiry(token)
	if err != nil || !expiresAt.After(now) {
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: token expiry is invalid", ErrAttestationRejected)
	}

	ingress, err := v.lookup.ByAttestor(ctx, namespace, serviceAccount)
	if errors.Is(err, ErrIngressUnavailable) {
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: attestor is not authorized", ErrAttestationRejected)
	}
	if err != nil {
		return Ingress{}, fmt.Errorf("lookup attestor ingress: %w", err)
	}

	cacheExpiry := now.Add(v.maxTTL)
	if expiresAt.Before(cacheExpiry) {
		cacheExpiry = expiresAt
	}
	v.storeCached(hash, cacheEntry{ingress: ingress, expiresAt: cacheExpiry, rejected: false, lastChecked: now})
	return ingress, nil
}

func (v *AttestationVerifier) cached(hash [32]byte, now time.Time) (cacheEntry, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[hash]
	if !ok {
		return cacheEntry{ingress: zeroIngress(), expiresAt: time.Time{}, rejected: false, lastChecked: time.Time{}}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(v.cache, hash)
		return cacheEntry{ingress: zeroIngress(), expiresAt: time.Time{}, rejected: false, lastChecked: time.Time{}}, false
	}
	return entry, true
}

func (v *AttestationVerifier) storeCached(hash [32]byte, entry cacheEntry) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sweepAndBoundCache(v.now())
	v.cache[hash] = entry
}

func (v *AttestationVerifier) storeRejected(hash [32]byte, now time.Time) {
	v.storeCached(hash, cacheEntry{ingress: Ingress{
		ID: uuid.Nil, OrganizationID: "", Provider: "", DNSName: "", IdentityRequired: false,
		AttestorNamespace: "", AttestorServiceAccount: "",
	}, expiresAt: now.Add(negativeCacheTTL), rejected: true, lastChecked: time.Time{}})
}

func (v *AttestationVerifier) sweepAndBoundCache(now time.Time) {
	for key, entry := range v.cache {
		if !now.Before(entry.expiresAt) {
			delete(v.cache, key)
		}
	}
	for len(v.cache) >= maxAttestationCacheSize {
		var candidate [32]byte
		var candidateEntry cacheEntry
		set := false
		for key, entry := range v.cache {
			if !set || (entry.rejected && !candidateEntry.rejected) || (entry.rejected == candidateEntry.rejected && entry.expiresAt.Before(candidateEntry.expiresAt)) {
				candidate = key
				candidateEntry = entry
				set = true
			}
		}
		if !set {
			break
		}
		delete(v.cache, candidate)
	}
}

func (v *AttestationVerifier) recheckCached(ctx context.Context, hash [32]byte) (Ingress, error) {
	value, err, _ := v.rechecks.Do(string(hash[:]), func() (any, error) {
		now := v.now()
		cached, ok := v.cached(hash, now)
		if !ok || cached.rejected {
			return zeroIngress(), fmt.Errorf("%w: cached authority expired", ErrAttestationRejected)
		}
		if now.Sub(cached.lastChecked) < servingStateRecheckTTL {
			return cached.ingress, nil
		}
		if err := v.lookup.Recheck(ctx, cached.ingress); err != nil {
			if errors.Is(err, ErrIngressUnavailable) || errors.Is(err, ErrIngressChanged) {
				v.deleteCached(hash)
				return zeroIngress(), fmt.Errorf("%w: ingress authority is no longer active", ErrAttestationRejected)
			}
			return zeroIngress(), fmt.Errorf("recheck cached private ingress authority: %w", err)
		}
		v.markChecked(hash, now)
		return cached.ingress, nil
	})
	if err != nil {
		return Ingress{}, err
	}
	ingress, ok := value.(Ingress)
	if !ok {
		return Ingress{}, errors.New("private ingress recheck returned an invalid result")
	}
	return ingress, nil
}

func (v *AttestationVerifier) markChecked(hash [32]byte, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[hash]
	if !ok || entry.rejected {
		return
	}
	entry.lastChecked = now
	v.cache[hash] = entry
}

func (v *AttestationVerifier) allowSource(source string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.globalLimiter == nil || !v.globalLimiter.Allow() {
		return false
	}
	limiter, ok := v.sourceLimiters[source]
	if !ok {
		if len(v.sourceLimiters) >= maxSourceLimiters {
			for key := range v.sourceLimiters {
				delete(v.sourceLimiters, key)
				break
			}
		}
		limiter = rate.NewLimiter(rate.Limit(100), 200)
		v.sourceLimiters[source] = limiter
	}
	return limiter.Allow()
}

func (v *AttestationVerifier) deleteCached(hash [32]byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.cache, hash)
}

func zeroIngress() Ingress {
	return Ingress{
		ID: uuid.Nil, OrganizationID: "", Provider: "", DNSName: "", IdentityRequired: false,
		AttestorNamespace: "", AttestorServiceAccount: "",
	}
}

func parseServiceAccountSubject(subject string) (string, string, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" || parts[2] == "" || parts[3] == "" {
		return "", "", fmt.Errorf("invalid service account subject")
	}
	return parts[2], parts[3], nil
}

func tokenExpiry(token string) (time.Time, error) {
	claims := jwt.RegisteredClaims{} //nolint:exhaustruct // ParseUnverified populates all relevant claims
	_, _, err := jwt.NewParser().ParseUnverified(token, &claims)
	if err != nil || claims.ExpiresAt == nil {
		return time.Time{}, fmt.Errorf("parse token expiry")
	}
	return claims.ExpiresAt.Time, nil
}

func containsAudience(audiences []string, expected string) bool {
	return slices.Contains(audiences, expected)
}
