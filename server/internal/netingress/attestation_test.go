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
	ingress        Ingress
	recheckErr     error
	byDNSNameCalls int
	byDNSNameErr   error
	recheckCalls   int
	host           string
	recheckStarted chan struct{}
	recheckRelease chan struct{}
	mu             sync.Mutex
}

// gatewaySubject is the service account newGatewayVerifier accepts.
const gatewaySubject = "system:serviceaccount:ns:sa"

// newGatewayVerifier accepts tokens for gatewaySubject, the tailnet gateway.
func newGatewayVerifier(t *testing.T, reviewer TokenReviewer, lookup GatewayIngressLookup, maxTTL time.Duration) *AttestationVerifier {
	t.Helper()
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, maxTTL)
	require.NoError(t, verifier.UseGatewayPrincipal("ns", "sa"))
	return verifier
}

// blockRechecks makes later rechecks signal started and wait for release.
func (f *fakeAttestorLookup) blockRechecks() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recheckStarted = make(chan struct{}, 1)
	f.recheckRelease = make(chan struct{})
}

func (f *fakeAttestorLookup) ByDNSName(_ context.Context, host string) (Ingress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byDNSNameCalls++
	f.host = host
	return f.ingress, f.byDNSNameErr
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
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: ingress}
	verifier := newGatewayVerifier(t, reviewer, lookup, 30*time.Second)
	verifier.now = func() time.Time { return now }

	got, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Len(t, reviewer.requests, 1)
	require.Equal(t, token, reviewer.requests[0].Spec.Token)
	require.Equal(t, []string{DefaultTokenAudience}, reviewer.requests[0].Spec.Audiences)
	require.Equal(t, "private.example.ts.net", lookup.host)

	got, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Len(t, reviewer.requests, 1, "cache hit must not repeat TokenReview")
	require.Equal(t, 1, lookup.byDNSNameCalls)
	require.Equal(t, 1, lookup.recheckCount(), "fresh cache hit stays inside the serving-state recheck interval")
}

func TestAttestationVerifierCacheBoundedByTokenExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	current := now
	token := unsignedToken(t, now.Add(5*time.Second))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return current }

	_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err)
	current = now.Add(4 * time.Second)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err)
	require.Len(t, reviewer.requests, 1)

	current = now.Add(5 * time.Second)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
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
		{name: "empty token", token: "", review: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)},
		{name: "review denied", token: validToken, review: &authenticationv1.TokenReview{}},
		{name: "wrong audience", token: validToken, review: authenticatedTokenReview("other", "system:serviceaccount:ns:sa")},
		{name: "wrong subject", token: validToken, review: authenticatedTokenReview(DefaultTokenAudience, "user@example.com")},
		{name: "expired", token: unsignedToken(t, now.Add(-time.Second)), review: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)},
		{name: "missing expiry", token: unsignedTokenWithoutExpiry(t), review: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)},
		{name: "review unavailable", token: validToken, reviewErr: errors.New("api unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reviewer := &fakeTokenReviewer{response: test.review, err: test.reviewErr}
			lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
			verifier := newGatewayVerifier(t, reviewer, lookup, time.Second)
			verifier.now = func() time.Time { return now }
			_, err := verifier.Verify(t.Context(), test.token, "10.0.0.1:1234", "private.example.ts.net")
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
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return current }

	invalid := unsignedToken(t, now.Add(time.Minute))
	_, err := verifier.Verify(t.Context(), invalid, "10.0.0.1:1234", "private.example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected)
	_, err = verifier.Verify(t.Context(), invalid, "10.0.0.1:1234", "private.example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 1, "negative cache must suppress repeated TokenReview calls")

	current = now.Add(negativeCacheTTL)
	_, err = verifier.Verify(t.Context(), invalid, "10.0.0.1:1234", "private.example.ts.net")
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
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return now }
	verifier.sourceLimiters["10.0.0.1"] = rate.NewLimiter(0, 0)

	_, err := verifier.Verify(t.Context(), token, "10.0.0.1", "private.example.ts.net")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAttestationRejected)
	require.Empty(t, reviewer.requests)

	_, err = verifier.Verify(t.Context(), token, "10.0.0.2", "private.example.ts.net")
	require.NoError(t, err, "one source limiter must not block another source or poison the token hash")
	require.Len(t, reviewer.requests, 1)
}

func TestAttestationVerifierLookupAndCacheRecheckFailures(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}}
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return current }

	_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err)

	lookup.recheckErr = errors.New("database unavailable")
	current = current.Add(servingStateRecheckTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.ErrorContains(t, err, "database unavailable")
	require.NotErrorIs(t, err, ErrAttestationRejected)
	lookup.recheckErr = nil
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err, "transient recheck failure must preserve the token")
	require.Len(t, reviewer.requests, 1)

	lookup.recheckErr = ErrIngressChanged
	current = current.Add(servingStateRecheckTTL)
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected)

	lookup.recheckErr = nil
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
	require.NoError(t, err, "the host is resolved again once its authority is restored")
	require.Len(t, reviewer.requests, 1, "a rejected host does not discard the reviewed token")
}

func TestAttestationVerifierCoalescesConcurrentRechecks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	const requests = 8
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New(), OrganizationID: "org_123"}}
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return current }
	require.NoError(t, func() error {
		_, err := verifier.Verify(t.Context(), token, "10.0.0.1", "private.example.ts.net")
		return err
	}())
	joined := make(chan struct{}, requests)
	verifier.recheckJoined = func() { joined <- struct{}{} }
	lookup.blockRechecks()
	current = now.Add(servingStateRecheckTTL)

	errorsCh := make(chan error, requests)
	for range requests {
		go func() {
			_, err := verifier.Verify(t.Context(), token, "10.0.0.1", "private.example.ts.net")
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
	require.Equal(t, 2, lookup.recheckCount(), "the first resolution, then one shared recheck")
}

func TestAttestationVerifierCanceledWaiterDoesNotCancelSharedRecheck(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	current := now
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New(), OrganizationID: "org_123"}}
	verifier := newGatewayVerifier(t, reviewer, lookup, time.Minute)
	verifier.now = func() time.Time { return current }
	_, err := verifier.Verify(t.Context(), token, "10.0.0.1", "private.example.ts.net")
	require.NoError(t, err)
	lookup.blockRechecks()
	current = now.Add(servingStateRecheckTTL)

	canceledCtx, cancel := context.WithCancel(t.Context())
	canceledErr := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(canceledCtx, token, "10.0.0.1", "private.example.ts.net")
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
		_, err := verifier.Verify(t.Context(), token, "10.0.0.1", "private.example.ts.net")
		healthyErr <- err
	}()
	close(lookup.recheckRelease)
	require.NoError(t, <-healthyErr)
	require.Equal(t, 2, lookup.recheckCount(), "the first resolution, then one shared recheck")
}

func TestAttestationVerifierGlobalRateLimitBoundsSourceChurn(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	verifier := newGatewayVerifier(t,
		&fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, gatewaySubject)},
		&fakeAttestorLookup{ingress: Ingress{ID: uuid.New()}},
		time.Minute,
	)
	verifier.now = func() time.Time { return now }
	verifier.globalLimiter = rate.NewLimiter(0, 1)

	_, err := verifier.Verify(t.Context(), unsignedToken(t, now.Add(time.Minute)), "10.0.0.1", "private.example.ts.net")
	require.NoError(t, err)
	_, err = verifier.Verify(t.Context(), unsignedTokenWithClaims(t, jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		ID:        "second",
	}), "10.0.0.2", "private.example.ts.net")
	require.ErrorContains(t, err, "rate limit")
}

func TestAttestationVerifierFailsClosedWhenUnconfigured(t *testing.T) {
	t.Parallel()

	verifier := NewAttestationVerifier(nil, nil, "", 0)
	_, err := verifier.Verify(t.Context(), "opaque", "10.0.0.1:1234", "private.example.ts.net")
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

func TestAttestationVerifierGatewayPrincipalResolvesByHost(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	token := unsignedToken(t, now.Add(time.Minute))
	ingress := Ingress{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale, DNSName: "speakeasy.example.ts.net"}
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:gram-gateway:gram-tailnet-gateway")}
	lookup := &fakeAttestorLookup{ingress: ingress}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, 30*time.Second)
	verifier.now = func() time.Time { return now }
	require.NoError(t, verifier.UseGatewayPrincipal("gram-gateway", "gram-tailnet-gateway"))
	require.Error(t, verifier.UseGatewayPrincipal("", "gram-tailnet-gateway"))

	got, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "Speakeasy.Example.ts.net:443")
	require.NoError(t, err)
	require.Equal(t, ingress, got)
	require.Equal(t, 1, lookup.byDNSNameCalls)
	require.Equal(t, 1, lookup.recheckCount(), "a fresh host resolution also confirms serving state")
	require.Equal(t, "speakeasy.example.ts.net", lookup.host, "the host is canonicalised before lookup")

	// Another host under the same token resolves without a second token review.
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "other.example.ts.net")
	require.NoError(t, err)
	require.Equal(t, 2, lookup.byDNSNameCalls)
	require.Len(t, reviewer.requests, 1, "host values must not force token reviews")

	// A cache hit for the first host repeats neither the review nor the lookup.
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "Speakeasy.Example.ts.net")
	require.NoError(t, err)
	require.Equal(t, 2, lookup.byDNSNameCalls)
	require.Len(t, reviewer.requests, 1)

	// Once the recheck interval passes, the host mapping is re-read and a
	// changed owner is rejected.
	verifier.now = func() time.Time { return now.Add(servingStateRecheckTTL) }
	lookup.ingress.ID = uuid.New()
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "Speakeasy.Example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Equal(t, 3, lookup.byDNSNameCalls)
	require.Len(t, reviewer.requests, 1)
}

func TestAttestationVerifierGatewayPrincipalRejectsUnknownHosts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	token := unsignedToken(t, now.Add(time.Minute))
	reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:gram-gateway:gram-tailnet-gateway")}
	lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale, DNSName: "speakeasy.example.ts.net"}}
	verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, 30*time.Second)
	verifier.now = func() time.Time { return now }
	require.NoError(t, verifier.UseGatewayPrincipal("gram-gateway", "gram-tailnet-gateway"))

	// No or ambiguous owner for the host: rejected, but the token stays valid.
	lookup.byDNSNameErr = ErrIngressUnavailable
	_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "unknown.example.ts.net")
	require.ErrorIs(t, err, ErrAttestationRejected)
	require.Len(t, reviewer.requests, 1)

	lookup.byDNSNameErr = nil
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "speakeasy.example.ts.net")
	require.NoError(t, err)
	require.Len(t, reviewer.requests, 1, "a rejected host does not poison the token")

	// A database failure is retryable, not a rejection.
	lookup.byDNSNameErr = errors.New("boom")
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "another.example.ts.net")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAttestationRejected)

	// An unparseable host never reaches the lookup.
	_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "bad host")
	require.ErrorIs(t, err, ErrAttestationRejected)
}

func TestAttestationVerifierRejectsOtherServiceAccounts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		gateway bool
	}{
		{name: "another workload", gateway: true},
		{name: "no gateway principal configured", gateway: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
			token := unsignedToken(t, now.Add(time.Minute))
			reviewer := &fakeTokenReviewer{response: authenticatedTokenReview(DefaultTokenAudience, "system:serviceaccount:other-ns:other-sa")}
			lookup := &fakeAttestorLookup{ingress: Ingress{ID: uuid.New(), OrganizationID: "org_123", Provider: ProviderTailscale, DNSName: "private.example.ts.net"}}
			verifier := NewAttestationVerifier(reviewer, lookup, DefaultTokenAudience, 30*time.Second)
			verifier.now = func() time.Time { return now }
			if test.gateway {
				require.NoError(t, verifier.UseGatewayPrincipal("gram-gateway", "gram-tailnet-gateway"))
			}

			_, err := verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
			require.ErrorIs(t, err, ErrAttestationRejected)
			_, err = verifier.Verify(t.Context(), token, "10.0.0.1:1234", "private.example.ts.net")
			require.ErrorIs(t, err, ErrAttestationRejected)
			require.Len(t, reviewer.requests, 1, "the rejection is negatively cached")
			require.Zero(t, lookup.byDNSNameCalls, "only the gateway's tokens resolve an ingress")
		})
	}
}
