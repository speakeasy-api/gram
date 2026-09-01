package netingress

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeTokenReviewer struct {
	response *authenticationv1.TokenReview
	err      error
	requests []*authenticationv1.TokenReview
}

func (f *fakeTokenReviewer) Create(_ context.Context, review *authenticationv1.TokenReview, _ metav1.CreateOptions) (*authenticationv1.TokenReview, error) {
	f.requests = append(f.requests, review.DeepCopy())
	return f.response, f.err
}

type fakeAttestorLookup struct {
	ingress         Ingress
	byAttestorErr   error
	recheckErr      error
	byAttestorCalls int
	recheckCalls    int
	namespace       string
	serviceAccount  string
	recheckStarted  chan struct{}
	recheckRelease  chan struct{}
	mu              sync.Mutex
}

func (f *fakeAttestorLookup) ByAttestor(_ context.Context, namespace, serviceAccount string) (Ingress, error) {
	f.byAttestorCalls++
	f.namespace = namespace
	f.serviceAccount = serviceAccount
	return f.ingress, f.byAttestorErr
}

func (f *fakeAttestorLookup) Recheck(_ context.Context, _ Ingress) error {
	f.mu.Lock()
	f.recheckCalls++
	started := f.recheckStarted
	release := f.recheckRelease
	err := f.recheckErr
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return err
}

func (f *fakeAttestorLookup) recheckCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recheckCalls
}

func TestAttestationVerifierSuccessAndCache(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	token := unsignedToken(t, now.Add(time.Minute))
	ingress := Ingress{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale, DNSName: "private.example.ts.net"}
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:attestor-ns:attestor-sa")}
	lookup := &fakeAttestorLookup{ingress: ingress}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, 30*time.Second)
	verifier.now = func() time.Time { return now }

	got, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Len(t, reviewer.requests, 1)
	require.Equal(t, token, reviewer.requests[0].Spec.Token)
	require.Equal(t, []string{DefaultTokenAudience}, reviewer.requests[0].Spec.Audiences)
	require.Equal(t, "attestor-ns", lookup.namespace)
	require.Equal(t, "attestor-sa", lookup.serviceAccount)

	got, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Len(t, reviewer.requests, 1, "cache hit must not repeat TokenReview")
	require.Equal(t, 1, lookup.byAttestorCalls)
	require.Zero(t, lookup.recheckCalls, "fresh cache hit stays inside the serving-state recheck interval")
}

func TestAttestationVerifierCacheBoundedByTokenExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	current := now
	token := unsignedToken(t, now.Add(5*time.Second))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return current }

	_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)
	current = now.Add(4 * time.Second)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)
	require.Len(t, reviewer.requests, 1)

	current = now.Add(5 * time.Second)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 2, "expired cache entry must perform a fresh TokenReview")
}

func TestAttestationVerifierRejectsInvalidReview(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	validToken := unsignedToken(t, now.Add(time.Minute))
	for _, test := range []struct {
		name      string
		token     string
		review    *authenticationv1.TokenReview
		reviewErr error
	}{
		{name: "empty token", token: "", review: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")},
		{name: "review denied", token: validToken, review: &authenticationv1.TokenReview{}},
		{name: "wrong audience", token: validToken, review: authenticatedTokenReview("other", "system:serviceaccount:ns:sa")},
		{name: "wrong subject", token: validToken, review: authenticatedTokenReview(DefaultTokenAudience, "user@example.com")},
		{name: "expired", token: unsignedToken(t, now.Add(-time.Second)), review: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")},
		{name: "missing expiry", token: unsignedTokenWithoutExpiry(t), review: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")},
		{name: "review unavailable", token: validToken, reviewErr: errors.New("api unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reviewer := &fakeTokenReviewer{response: test.review, err: test.reviewErr}
			lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
			verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Second)
			verifier.now = func() time.Time { return now }
			_, err := verifier.Verify(t.Context(), test.token, "10.0.0.1:1234")
			require.Error(t, err)
			if test.token != "" {
				require.NotContains(t, err.Error(), test.token, "attestation token must never appear in errors")
			}
		})
	}
}

func TestAttestationVerifierNegativeCacheAndBoundedSize(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	reviewer := &fakeTokenReviewer{response: &authenticationv1.TokenReview{}}
	lookup := &fakeAttestorLookup{}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return current }

	invalid := unsignedToken(t, now.Add(time.Minute))
	_, err := verifier.Verify(t.Context(), invalid, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)
	_, err = verifier.Verify(t.Context(), invalid, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 1, "negative cache must suppress repeated TokenReview calls")

	current = now.Add(negativeCacheTTL)
	_, err = verifier.Verify(t.Context(), invalid, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 2, "expired negative cache entry must permit a fresh review")

	verifier.mu.Lock()
	for i := range maxAttestationCacheSize + 100 {
		hash := sha256.Sum256(fmt.Appendf(nil, "token-%d", i))
		verifier.cache[hash] = cacheEntry{expiresAt: current.Add(time.Minute)}
	}
	verifier.sweepAndBoundCache(current)
	require.LessOrEqual(t, len(verifier.cache), maxAttestationCacheSize-1)
	verifier.mu.Unlock()
}

func TestAttestationVerifierRateLimitIsRetryableAndNotCached(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return now }
	verifier.sourceLimiters["10.0.0.1"] = rate.NewLimiter(0, 0)

	_, err := verifier.Verify(t.Context(), token, "10.0.0.1")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAttestationRejected)
	require.Empty(t, reviewer.requests)

	_, err = verifier.Verify(t.Context(), token, "10.0.0.2")
	require.NoError(t, err, "one source limiter must not block another source or poison the token hash")
	require.Len(t, reviewer.requests, 1)
}

func TestAttestationVerifierLookupAndCacheRecheckFailures(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return current }

	lookup.byAttestorErr = ErrIngressUnavailable
	_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)

	lookup.byAttestorErr = nil
	current = now.Add(negativeCacheTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)

	lookup.recheckErr = errors.New("database unavailable")
	current = current.Add(servingStateRecheckTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.ErrorContains(t, err, "database unavailable")
	require.NotErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 2)
	lookup.recheckErr = nil
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err, "transient recheck failure must preserve the positive cache")
	require.Len(t, reviewer.requests, 2)

	lookup.recheckErr = ErrIngressChanged
	current = current.Add(servingStateRecheckTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)

	lookup.recheckErr = nil
	current = current.Add(negativeCacheTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234")
	require.NoError(t, err)
	require.Len(t, reviewer.requests, 3, "rejected cache entry must be removed before retry")
}

func TestAttestationVerifierCoalescesConcurrentRechecks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	const requests = 8
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}
	lookup := &fakeAttestorLookup{
		ingress:        Ingress{ID: uuid.New(), OrganizationID: "org_123"},
		recheckStarted: make(chan struct{}, 1),
		recheckRelease: make(chan struct{}),
	}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return current }
	joined := make(chan struct{}, requests)
	verifier.recheckJoined = func() { joined <- struct{}{} }
	require.NoError(t, func() error {
		_, err := verifier.Verify(t.Context(), token, "10.0.0.1")
		return err
	}())
	current = now.Add(servingStateRecheckTTL)

	errorsCh := make(chan error, requests)
	for range requests {
		go func() {
			_, err := verifier.Verify(t.Context(), token, "10.0.0.1")
			errorsCh <- err
		}()
	}
	select {
	case <-lookup.recheckStarted:
	case <-time.After(time.Second):
		t.Fatal("recheck did not start")
	}
	for range requests {
		select {
		case <-joined:
		case <-time.After(time.Second):
			t.Fatal("concurrent caller did not join the recheck")
		}
	}
	close(lookup.recheckRelease)
	for range requests {
		require.NoError(t, <-errorsCh)
	}
	require.Equal(t, 1, lookup.recheckCount())
}

func TestAttestationVerifierCanceledWaiterDoesNotCancelSharedRecheck(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")}
	lookup := &fakeAttestorLookup{
		ingress:        Ingress{ID: uuid.New(), OrganizationID: "org_123"},
		recheckStarted: make(chan struct{}, 1),
		recheckRelease: make(chan struct{}),
	}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, time.Minute)
	verifier.now = func() time.Time { return current }
	_, err := verifier.Verify(t.Context(), token, "10.0.0.1")
	require.NoError(t, err)
	current = now.Add(servingStateRecheckTTL)

	canceledCtx, cancel := context.WithCancel(t.Context())
	canceledErr := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(canceledCtx, token, "10.0.0.1")
		canceledErr <- err
	}()
	select {
	case <-lookup.recheckStarted:
	case <-time.After(time.Second):
		t.Fatal("recheck did not start")
	}
	cancel()
	require.ErrorIs(t, <-canceledErr, context.Canceled)

	healthyErr := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(t.Context(), token, "10.0.0.1")
		healthyErr <- err
	}()
	close(lookup.recheckRelease)
	require.NoError(t, <-healthyErr)
	require.Equal(t, 1, lookup.recheckCount())
}

func TestAttestationVerifierGlobalRateLimitBoundsSourceChurn(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	verifier := NewAttestationVerifier(
		&fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:ns:sa")},
		&fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}},
		DefaultTokenAudience,
		time.Minute,
	)
	verifier.now = func() time.Time { return now }
	verifier.globalLimiter = rate.NewLimiter(0, 1)

	_, err := verifier.Verify(t.Context(), unsignedToken(t, now.Add(time.Minute)), "10.0.0.1")
	require.NoError(t, err)
	_, err = verifier.Verify(t.Context(), unsignedTokenWithClaims(t, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		ID:        "second",
	}), "10.0.0.2")
	require.ErrorContains(t, err, "rate limit")
}

func TestAttestationVerifierFailsClosedWhenUnconfigured(t *testing.T) {
	t.Parallel()

	verifier := NewAttestationVerifier(nil, nil, "", 0)
	_, err := verifier.Verify(t.Context(), "opaque", "10.0.0.1:1234")
	require.ErrorIs(t, err, ErrAttestationRejected)
}

func authenticatedTokenReview(audience, subject string) *authenticationv1.TokenReview {
	return &authenticationv1.TokenReview{Status: authenticationv1.TokenReviewStatus{
		Authenticated: true,
		Audiences:     []string{audience},
		User:          authenticationv1.UserInfo{Username: subject},
	}}
}

func unsignedToken(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	claims := jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(expiresAt)}
	return unsignedTokenWithClaims(t, claims)
}

func unsignedTokenWithoutExpiry(t *testing.T) string {
	t.Helper()
	return unsignedTokenWithClaims(t, jwt.RegisteredClaims{})
}

func unsignedTokenWithClaims(t *testing.T, claims jwt.RegisteredClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return signed
}
