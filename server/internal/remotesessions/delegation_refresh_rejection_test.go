package remotesessions

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// refusalRetryWait is past the delegation service's retry delay, whose value is private to it.
const refusalRetryWait = 10 * time.Minute

// fakeTokenEndpoint answers each refresh with the current response and records
// the refresh token it was sent.
type fakeTokenEndpoint struct {
	mu        sync.Mutex
	status    int
	body      string
	submitted []string
}

func (e *fakeTokenEndpoint) answer(status int, body string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status, e.body = status, body
}

func (e *fakeTokenEndpoint) received() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.submitted...)
}

// rejectionFixture drives the real Postgres store and the real HTTP client
// against a fake provider. The clock starts refusalRetryWait in the past
// because the claim query checks retry_after against the database clock.
func rejectionFixture(t *testing.T, locks cache.Cache) (*delegationAdapterFixture, *delegationAdapterStore, DelegationBinding, DelegationAuthorizer, *fakeTokenEndpoint, *bytes.Buffer) {
	t.Helper()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	endpoint := &fakeTokenEndpoint{}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read refresh request: %v", err)
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Errorf("parse refresh request: %v", err)
		}
		endpoint.mu.Lock()
		defer endpoint.mu.Unlock()
		endpoint.submitted = append(endpoint.submitted, form.Get("refresh_token"))
		w.WriteHeader(endpoint.status)
		_, _ = io.WriteString(w, endpoint.body)
	}))
	t.Cleanup(upstream.Close)
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	p.issuer.Issuer = upstream.URL
	p.metadata.Issuer = upstream.URL
	p.metadata.AuthorizationEndpoint = upstream.URL + "/authorize"
	p.metadata.TokenEndpoint = upstream.URL + "/token"
	p.metadata.JwksURI = upstream.URL + "/jwks"
	p.client.ClientSecretEncrypted.String, err = s.enc.Encrypt([]byte("secret-client"))
	require.NoError(t, err)
	logs := &bytes.Buffer{}
	manager := &ChallengeManager{enc: s.enc, policy: policy, logger: slog.New(slog.NewJSONHandler(logs, nil)), locks: locks}
	s.refreshIdentity = manager.RefreshFederatedIdentity
	s.refreshPaused = manager.federatedClientPaused
	start := time.Now().UTC().Truncate(time.Microsecond).Add(-refusalRetryWait)
	s.now = func() time.Time { return start }
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, start, "secret-id", "secret-refresh", 30*time.Second), true))
	return s, store, b, allow, endpoint, logs
}

func TestDelegationUnexpectedRejectionKeepsTokenUntilProviderRecovers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid client", status: http.StatusUnauthorized, body: `{"error":"invalid_client","error_description":"secret-upstream"}`},
		{name: "unauthorized client", status: http.StatusBadRequest, body: `{"error":"unauthorized_client","error_description":"secret-upstream"}`},
		{name: "html forbidden", status: http.StatusForbidden, body: `<html>secret-upstream</html>`},
		{name: "endpoint not found", status: http.StatusNotFound, body: ``},
		{name: "unknown code", status: http.StatusBadRequest, body: `{"error":"secret-upstream"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, b, allow, endpoint, logs := rejectionFixture(t, nil)
			endpoint.answer(tc.status, tc.body)

			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			row, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "configuration_failure", row.ObservationStatus.String)
			require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
			require.False(t, row.RefreshClaimID.Valid)

			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Len(t, endpoint.received(), 1, "the pause must hold without another provider request")

			endpoint.answer(http.StatusOK, `{"access_token":"secret-access","refresh_token":"secret-rotated"}`)
			recovered := s.now().Add(refusalRetryWait)
			s.now = func() time.Time { return recovered }
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication, "this response has no ID token to renew the assertion with")
			require.Equal(t, []string{"secret-refresh", "secret-refresh"}, endpoint.received())
			row, err = store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "secret-rotated", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
			require.NotEqual(t, "configuration_failure", row.ObservationStatus.String)

			require.Contains(t, logs.String(), "federated refresh rejected")
			require.NotContains(t, logs.String(), "secret-")
		})
	}
}

type unavailableCache struct{ cache.Cache }

func (unavailableCache) Get(context.Context, string, any) error {
	return errors.New("cache unavailable")
}

func (unavailableCache) Set(context.Context, string, any, time.Duration) error {
	return errors.New("cache unavailable")
}

func moveDelegationClock(s *delegationAdapterFixture) {
	later := s.now().Add(refusalRetryWait)
	s.now = func() time.Time { return later }
}

func TestDelegationClientRejectionPausesEveryRefreshForThatClient(t *testing.T) {
	t.Parallel()
	locks := testenv.NewMemoryCache()
	s, store, b, allow, endpoint, _ := rejectionFixture(t, locks)
	endpoint.answer(http.StatusUnauthorized, `{"error":"invalid_client"}`)

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Len(t, endpoint.received(), 1)
	before, err := store.load(t.Context(), b)
	require.NoError(t, err)

	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Len(t, endpoint.received(), 1, "the client pause must stop the request")
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, "secret-refresh", delegationPlain(t, s, after.RefreshTokenEncrypted.String))
}

func TestDelegationClientPauseLeavesHealthyRowUnchanged(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	locks := cache.NewRedisCacheAdapter(rc)
	s, store, b, allow, endpoint, _ := rejectionFixture(t, locks)
	p, err := s.loadProvider(t.Context(), b.OrganizationID, b.IssuerID, b.ClientID)
	require.NoError(t, err)
	// The assertion is inside the renewal window but unexpired, so no expiry cleanup runs.
	now := time.Now().UTC().Truncate(time.Microsecond)
	s.now = func() time.Time { return now }
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, now, "secret-id", "secret-refresh", 30*time.Second), true))
	before, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "durable_credential_present", before.ObservationStatus.String)
	require.NoError(t, locks.Set(t.Context(), federatedClientPauseKey(p), true, federatedClientPause))

	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Empty(t, endpoint.received())
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, before, after, "the row must not change, including its generation and timestamps")

	mr.FastForward(federatedClientPause)
	endpoint.answer(http.StatusOK, `{"access_token":"secret-access","refresh_token":"secret-rotated"}`)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication, "this response has no ID token to renew the assertion with")
	require.Equal(t, []string{"secret-refresh"}, endpoint.received())
	after, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "secret-rotated", delegationPlain(t, s, after.RefreshTokenEncrypted.String))
}

func TestFederatedClientPauseIsNotExtendedByLateRejections(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rc.Close()) })
	logger := testenv.NewLogger(t)
	m := &ChallengeManager{locks: cache.NewRedisCacheAdapter(rc), logger: logger}
	p := federatedFixture(t)
	m.pauseFederatedClient(t.Context(), logger, p)
	require.True(t, m.federatedClientPaused(t.Context(), p))
	mr.FastForward(federatedClientPause - time.Second)
	m.pauseFederatedClient(t.Context(), logger, p)
	require.True(t, m.federatedClientPaused(t.Context(), p))
	mr.FastForward(time.Second)
	require.False(t, m.federatedClientPaused(t.Context(), p))
}

func TestDelegationOtherRejectionDoesNotPauseTheClient(t *testing.T) {
	t.Parallel()
	s, _, b, allow, endpoint, _ := rejectionFixture(t, testenv.NewMemoryCache())
	endpoint.answer(http.StatusForbidden, `<html>blocked</html>`)

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Len(t, endpoint.received(), 2)
}

func TestDelegationClientPauseFailsOpenWhenCacheIsDown(t *testing.T) {
	t.Parallel()
	s, _, b, allow, endpoint, _ := rejectionFixture(t, unavailableCache{})
	endpoint.answer(http.StatusUnauthorized, `{"error":"invalid_client"}`)

	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)
	require.Len(t, endpoint.received(), 2)
}

func TestDelegationRetainedTokenSurvivesRateLimitedRetry(t *testing.T) {
	t.Parallel()
	s, store, b, allow, endpoint, _ := rejectionFixture(t, nil)
	endpoint.answer(http.StatusForbidden, `<html>blocked</html>`)
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationConfiguration)

	endpoint.answer(http.StatusTooManyRequests, `slow down`)
	moveDelegationClock(s)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	row, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.False(t, row.RefreshClaimID.Valid, "a rate limit must not hold the claim")
	require.Equal(t, "configuration_failure", row.ObservationStatus.String)
	require.Equal(t, "secret-refresh", delegationPlain(t, s, row.RefreshTokenEncrypted.String))
}

func TestDelegationInvalidGrantClearsSavedToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "standard", body: `{"error":"invalid_grant","error_description":"secret-upstream"}`},
		{name: "vendor envelope", body: `{"errors":["invalid_grant - secret-upstream"]}`},
		{name: "login required", body: `{"error":"login_required","error_description":"secret-upstream"}`},
		{name: "interaction required", body: `{"error":"interaction_required","error_description":"secret-upstream"}`},
		{name: "consent required", body: `{"error":"consent_required","error_description":"secret-upstream"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, b, allow, endpoint, logs := rejectionFixture(t, nil)
			endpoint.answer(http.StatusBadRequest, tc.body)

			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication)
			row, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Empty(t, row.RefreshTokenEncrypted.String)
			require.Equal(t, "reauthentication_required", row.ObservationStatus.String)

			later := s.now().Add(refusalRetryWait)
			s.now = func() time.Time { return later }
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationReauthentication)
			require.Len(t, endpoint.received(), 1)
			require.NotContains(t, logs.String(), "secret-")
		})
	}
}
