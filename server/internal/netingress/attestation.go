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

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	DefaultTokenAudience       = "gram-netingress" //nolint:gosec // public TokenReview audience, not a credential
	maxAttestationBytes        = 16 * 1024
	maxAttestationCacheSize    = 4096
	negativeCacheTTL           = 2 * time.Second
	servingStateRecheckTTL     = 2 * time.Second
	servingStateRecheckTimeout = 5 * time.Second
	maxSourceLimiters          = 4096
	globalReviewRate           = rate.Limit(500)
	globalReviewBurst          = 1000
)

var ErrAttestationRejected = errors.New("private ingress attestation rejected")

type TokenReviewer interface {
	Create(context.Context, *authenticationv1.TokenReview, metav1.CreateOptions) (*authenticationv1.TokenReview, error)
}

type AttestorIngressLookup interface {
	ByAttestor(context.Context, string, string) (Ingress, error)
	ByDNSName(context.Context, string) (Ingress, error)
	Recheck(context.Context, Ingress) error
}

type cacheEntry struct {
	ingress     Ingress
	expiresAt   time.Time
	rejected    bool
	lastChecked time.Time

	// gateway marks a token belonging to the shared tailnet gateway. Its
	// ingress depends on the request host, so hosts holds one resolution per
	// canonical host instead of the single ingress above.
	gateway bool
	hosts   map[string]hostEntry
}

// hostEntry is one host's resolved ingress under a gateway token.
type hostEntry struct {
	ingress     Ingress
	lastChecked time.Time
}

// maxGatewayHostsPerToken bounds host resolutions cached under one gateway
// token. A gateway serves at most the ingresses it hosts, so this is ample,
// and it stops a flood of unknown hosts from growing the entry.
const maxGatewayHostsPerToken = 256

type AttestationVerifier struct {
	reviewer TokenReviewer
	lookup   AttestorIngressLookup
	audience string
	maxTTL   time.Duration
	now      func() time.Time

	// gatewayNamespace and gatewayServiceAccount identify the shared tailnet
	// gateway. Its requests resolve the ingress from the Host header instead of
	// from a per-ingress attestor identity. Empty disables the gateway path.
	gatewayNamespace      string
	gatewayServiceAccount string

	mu             sync.Mutex
	cache          map[[32]byte]cacheEntry
	sourceLimiters map[string]*rate.Limiter
	globalLimiter  *rate.Limiter
	rechecks       singleflight.Group
	recheckJoined  func()
	telemetry      *Telemetry
}

func NewAttestationVerifier(reviewer TokenReviewer, lookup AttestorIngressLookup, audience string, maxTTL time.Duration, telemetry ...*Telemetry) *AttestationVerifier {
	if audience == "" {
		audience = DefaultTokenAudience
	}
	if maxTTL <= 0 {
		maxTTL = 30 * time.Second
	}
	var metrics *Telemetry
	if len(telemetry) > 0 {
		metrics = telemetry[0]
	}
	return &AttestationVerifier{
		reviewer:              reviewer,
		lookup:                lookup,
		audience:              audience,
		maxTTL:                maxTTL,
		now:                   time.Now,
		gatewayNamespace:      "",
		gatewayServiceAccount: "",
		mu:                    sync.Mutex{},
		cache:                 make(map[[32]byte]cacheEntry),
		sourceLimiters:        make(map[string]*rate.Limiter),
		globalLimiter:         rate.NewLimiter(globalReviewRate, globalReviewBurst),
		rechecks:              singleflight.Group{},
		recheckJoined:         nil,
		telemetry:             metrics,
	}
}

// UseGatewayPrincipal accepts the tailnet gateway's service account as an
// attestor for any gateway-hosted ingress, resolved by request host.
func (v *AttestationVerifier) UseGatewayPrincipal(namespace, serviceAccount string) error {
	if namespace == "" || serviceAccount == "" {
		return errors.New("tailnet gateway principal requires a namespace and service account")
	}
	v.gatewayNamespace = namespace
	v.gatewayServiceAccount = serviceAccount
	return nil
}

func (v *AttestationVerifier) isGatewayPrincipal(namespace, serviceAccount string) bool {
	return v.gatewayNamespace != "" && namespace == v.gatewayNamespace && serviceAccount == v.gatewayServiceAccount
}

// Verify authenticates an attestation token for a request to host. Per-ingress
// attestors are resolved from their service account. The shared tailnet
// gateway is resolved from host; its cache entry is still keyed by token so
// host values cannot force repeated token reviews, and each host's resolution
// is cached and rechecked beneath that entry.
func (v *AttestationVerifier) Verify(ctx context.Context, token, source, host string) (ingress Ingress, err error) {
	started := time.Now()
	result, reason, provider := ResultError, ReasonDependencyFailed, ""
	defer func() {
		if err == nil {
			result = ResultAllowed
			provider = ingress.Provider
		} else if reason != ReasonVerifierUnavailable && (errors.Is(err, ErrAttestationRejected) || reason == ReasonRateLimited) {
			result = ResultDenied
		}
		v.telemetry.Record(ctx, OperationAttestation, result, reason, provider, time.Since(started))
	}()
	if v.reviewer == nil || v.lookup == nil {
		reason = ReasonVerifierUnavailable
		return Ingress{}, fmt.Errorf("%w: verifier is not configured", ErrAttestationRejected)
	}
	if token == "" || strings.TrimSpace(token) != token || len(token) > maxAttestationBytes || source == "" {
		reason = ReasonAttestationRejected
		return Ingress{}, fmt.Errorf("%w: invalid bearer token", ErrAttestationRejected)
	}

	hash := sha256.Sum256([]byte(token))
	now := v.now()
	if cached, ok := v.cached(hash, now); ok {
		if cached.rejected {
			reason = ReasonNegativeCacheHit
			return Ingress{}, fmt.Errorf("%w: token was recently rejected", ErrAttestationRejected)
		}
		if cached.gateway {
			ingress, reason, err = v.gatewayHost(ctx, hash, host, now)
			if err == nil {
				provider = ingress.Provider
			}
			return ingress, err
		}
		if now.Sub(cached.lastChecked) >= servingStateRecheckTTL {
			provider = cached.ingress.Provider
			ingress, err = v.recheckCached(ctx, hash)
			if err != nil {
				if errors.Is(err, ErrAttestationRejected) {
					reason = ReasonAuthorityRejected
				}
				return Ingress{}, err
			}
			reason = ReasonCacheHit
			return ingress, nil
		}
		reason = ReasonCacheHit
		return cached.ingress, nil
	}

	if !v.allowSource(source) {
		reason = ReasonRateLimited
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
		reason = ReasonDependencyFailed
		return Ingress{}, fmt.Errorf("review private ingress token: %w", err)
	}
	if review == nil || !review.Status.Authenticated || review.Status.Error != "" || !containsAudience(review.Status.Audiences, v.audience) {
		reason = ReasonTokenReviewDenied
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: token review denied", ErrAttestationRejected)
	}

	namespace, serviceAccount, err := parseServiceAccountSubject(review.Status.User.Username)
	if err != nil {
		reason = ReasonAttestationRejected
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: %w", ErrAttestationRejected, err)
	}
	expiresAt, err := tokenExpiry(token)
	if err != nil || !expiresAt.After(now) {
		reason = ReasonAttestationRejected
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: token expiry is invalid", ErrAttestationRejected)
	}

	cacheExpiry := now.Add(v.maxTTL)
	if expiresAt.Before(cacheExpiry) {
		cacheExpiry = expiresAt
	}
	if v.isGatewayPrincipal(namespace, serviceAccount) {
		// The token is valid for any gateway-hosted ingress; which one is
		// decided per host below and cached under this entry.
		v.storeCached(hash, cacheEntry{ingress: zeroIngress(), expiresAt: cacheExpiry, rejected: false, lastChecked: now, gateway: true, hosts: make(map[string]hostEntry)})
		ingress, reason, err = v.gatewayHost(ctx, hash, host, now)
		if err == nil {
			provider = ingress.Provider
		}
		return ingress, err
	}

	ingress, err = v.lookup.ByAttestor(ctx, namespace, serviceAccount)
	if errors.Is(err, ErrIngressUnavailable) {
		reason = ReasonAuthorityRejected
		v.storeRejected(hash, now)
		return Ingress{}, fmt.Errorf("%w: attestor is not authorized", ErrAttestationRejected)
	}
	if err != nil {
		reason = ReasonDependencyFailed
		return Ingress{}, fmt.Errorf("lookup attestor ingress: %w", err)
	}

	reason = ReasonNone
	v.storeCached(hash, cacheEntry{ingress: ingress, expiresAt: cacheExpiry, rejected: false, lastChecked: now, gateway: false, hosts: nil})
	return ingress, nil
}

// gatewayHost resolves host under a cached gateway token. A fresh resolution
// is served from the entry; a stale or missing one re-reads the host mapping,
// which also confirms the ingress is still the single enabled gateway owner
// of that name, before rechecking its serving state.
func (v *AttestationVerifier) gatewayHost(ctx context.Context, hash [32]byte, host string, now time.Time) (Ingress, string, error) {
	canonical, err := requestorigin.CanonicalHost(host)
	if err != nil || canonical == "" {
		return Ingress{}, ReasonAuthorityRejected, fmt.Errorf("%w: invalid request host", ErrAttestationRejected)
	}
	if cached, ok := v.cachedHost(hash, canonical); ok && now.Sub(cached.lastChecked) < servingStateRecheckTTL {
		return cached.ingress, ReasonCacheHit, nil
	}

	results := v.rechecks.DoChan(string(hash[:])+"\x00"+canonical, func() (any, error) {
		recheckCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), servingStateRecheckTimeout)
		defer cancel()
		previous, had := v.cachedHost(hash, canonical)
		ingress, err := v.lookup.ByDNSName(recheckCtx, canonical)
		if errors.Is(err, ErrIngressUnavailable) || (had && err == nil && ingress.ID != previous.ingress.ID) {
			v.deleteHost(hash, canonical)
			return zeroIngress(), fmt.Errorf("%w: host is not served by the gateway", ErrAttestationRejected)
		}
		if err != nil {
			return zeroIngress(), fmt.Errorf("lookup gateway ingress by host: %w", err)
		}
		if err := v.lookup.Recheck(recheckCtx, ingress); err != nil {
			if errors.Is(err, ErrIngressUnavailable) || errors.Is(err, ErrIngressChanged) {
				v.deleteHost(hash, canonical)
				return zeroIngress(), fmt.Errorf("%w: ingress authority is no longer active", ErrAttestationRejected)
			}
			return zeroIngress(), fmt.Errorf("recheck gateway ingress authority: %w", err)
		}
		v.storeHost(hash, canonical, hostEntry{ingress: ingress, lastChecked: v.now()})
		return ingress, nil
	})
	if v.recheckJoined != nil {
		v.recheckJoined()
	}
	var result singleflight.Result
	select {
	case result = <-results:
	case <-ctx.Done():
		return Ingress{}, ReasonDependencyFailed, fmt.Errorf("wait for gateway host resolution: %w", ctx.Err())
	}
	if result.Err != nil {
		if errors.Is(result.Err, ErrAttestationRejected) {
			return Ingress{}, ReasonAuthorityRejected, result.Err
		}
		return Ingress{}, ReasonDependencyFailed, result.Err
	}
	ingress, ok := result.Val.(Ingress)
	if !ok {
		return Ingress{}, ReasonDependencyFailed, errors.New("gateway host resolution returned an invalid result")
	}
	return ingress, ReasonNone, nil
}

func (v *AttestationVerifier) cachedHost(hash [32]byte, host string) (hostEntry, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[hash]
	if !ok || !entry.gateway {
		return hostEntry{ingress: zeroIngress(), lastChecked: time.Time{}}, false
	}
	cached, ok := entry.hosts[host]
	return cached, ok
}

func (v *AttestationVerifier) storeHost(hash [32]byte, host string, cached hostEntry) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[hash]
	if !ok || !entry.gateway {
		return
	}
	if _, exists := entry.hosts[host]; !exists && len(entry.hosts) >= maxGatewayHostsPerToken {
		for key := range entry.hosts {
			delete(entry.hosts, key)
			break
		}
	}
	entry.hosts[host] = cached
}

func (v *AttestationVerifier) deleteHost(hash [32]byte, host string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if entry, ok := v.cache[hash]; ok && entry.gateway {
		delete(entry.hosts, host)
	}
}

func (v *AttestationVerifier) cached(hash [32]byte, now time.Time) (cacheEntry, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[hash]
	if !ok {
		return cacheEntry{ingress: zeroIngress(), expiresAt: time.Time{}, rejected: false, lastChecked: time.Time{}, gateway: false, hosts: nil}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(v.cache, hash)
		return cacheEntry{ingress: zeroIngress(), expiresAt: time.Time{}, rejected: false, lastChecked: time.Time{}, gateway: false, hosts: nil}, false
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
	}, expiresAt: now.Add(negativeCacheTTL), rejected: true, lastChecked: time.Time{}, gateway: false, hosts: nil})
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
	results := v.rechecks.DoChan(string(hash[:]), func() (any, error) {
		now := v.now()
		cached, ok := v.cached(hash, now)
		if !ok || cached.rejected {
			return zeroIngress(), fmt.Errorf("%w: cached authority expired", ErrAttestationRejected)
		}
		if now.Sub(cached.lastChecked) < servingStateRecheckTTL {
			return cached.ingress, nil
		}
		recheckCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), servingStateRecheckTimeout)
		defer cancel()
		if err := v.lookup.Recheck(recheckCtx, cached.ingress); err != nil {
			if errors.Is(err, ErrIngressUnavailable) || errors.Is(err, ErrIngressChanged) {
				v.deleteCached(hash)
				return zeroIngress(), fmt.Errorf("%w: ingress authority is no longer active", ErrAttestationRejected)
			}
			return zeroIngress(), fmt.Errorf("recheck cached private ingress authority: %w", err)
		}
		v.markChecked(hash, now)
		return cached.ingress, nil
	})
	if v.recheckJoined != nil {
		v.recheckJoined()
	}
	var result singleflight.Result
	select {
	case result = <-results:
	case <-ctx.Done():
		return Ingress{}, fmt.Errorf("wait for private ingress recheck: %w", ctx.Err())
	}
	if result.Err != nil {
		return Ingress{}, result.Err
	}
	ingress, ok := result.Val.(Ingress)
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
