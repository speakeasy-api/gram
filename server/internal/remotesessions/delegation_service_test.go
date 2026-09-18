package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/stretchr/testify/require"
)

// CAS advances the generation on credential writes, including claim acquisition.
// All state is copied under the mutex so refresh/callback interleavings are real.
type delegationMemoryStore struct {
	mu              sync.Mutex
	rows            map[DelegationBinding]delegationCredential
	claimError      error
	finishError     error
	attemptError    error
	attemptConflict bool
	attemptedAt     time.Time
	trustRemoved    bool
	claims          int
}

func (s *delegationMemoryStore) load(_ context.Context, b DelegationBinding) (delegationCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[b]
	if !ok || s.trustRemoved {
		return delegationCredential{}, pgx.ErrNoRows
	}
	return c, nil
}
func (s *delegationMemoryStore) save(_ context.Context, b DelegationBinding, g int64, next delegationCredential) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows[b].generation != g {
		return false, nil
	}
	next.generation = g + 1
	// The callback upsert clears claims; refusal must not use it on an active claim.
	next.claim = uuid.Nil
	s.rows[b] = next
	return true, nil
}
func (s *delegationMemoryStore) claim(_ context.Context, b DelegationBinding, g int64, id uuid.UUID, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims++
	if s.claimError != nil {
		return false, s.claimError
	}
	c, ok := s.rows[b]
	if !ok || c.generation != g || c.claim != uuid.Nil {
		return false, nil
	}
	c.claim = id
	c.generation++
	s.rows[b] = c
	return true, nil
}
func (s *delegationMemoryStore) markRefreshAttempt(_ context.Context, b DelegationBinding, g int64, id uuid.UUID, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attemptError != nil {
		return false, s.attemptError
	}
	c, ok := s.rows[b]
	if !ok || c.generation != g || c.claim != id || s.attemptConflict {
		return false, nil
	}
	s.attemptedAt = now
	return true, nil
}
func (s *delegationMemoryStore) revoke(_ context.Context, b DelegationBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[b]
	if !ok {
		return nil
	}
	c = clearDelegationSecrets(c)
	c.generation++
	c.status = "reauthentication_required"
	s.rows[b] = c
	return nil
}
func (s *delegationMemoryStore) finish(_ context.Context, b DelegationBinding, g int64, id uuid.UUID, next delegationCredential) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishError != nil {
		return false, s.finishError
	}
	c, ok := s.rows[b]
	if !ok || c.generation != g || c.claim != id {
		return false, nil
	}
	next.generation = g + 1
	s.rows[b] = next
	return true, nil
}
func (s *delegationMemoryStore) clearExpired(_ context.Context, b DelegationBinding, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[b]
	if !ok {
		return nil
	}
	// Repository expiry cleanup erases only the assertion without changing CAS.
	if c.assertion != "" && !c.assertionExpiry.After(now) {
		c.assertion = ""
		c.assertionExpiry = time.Time{}
		s.rows[b] = c
	}
	return nil
}

type delegationTestAuthority func(context.Context, DelegationBinding) error

func (f delegationTestAuthority) AuthorizeDelegation(ctx context.Context, b DelegationBinding) error {
	return f(ctx, b)
}

func newDelegationUnitFixture(t *testing.T) (*DelegationService, *delegationMemoryStore, *FederatedProvider, DelegationBinding, DelegationAuthorizer) {
	t.Helper()
	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)
	p := federatedFixture(t)
	b := delegationBinding(p, "human-test")
	require.True(t, validDelegationBinding(b))
	store := &delegationMemoryStore{rows: make(map[DelegationBinding]delegationCredential)}
	now := time.Unix(1700000000, 0)
	service := &DelegationService{store: store, enc: enc, now: func() time.Time { return now }}
	service.loadProvider = func(_ context.Context, org string, issuer, client uuid.UUID) (*FederatedProvider, error) {
		if org != p.organizationID || issuer != p.issuer.ID || client != p.client.ID {
			return nil, ErrFederatedConfiguration
		}
		return p, nil
	}
	service.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		return nil, errors.New("unexpected refresh")
	}
	allow := delegationTestAuthority(func(_ context.Context, got DelegationBinding) error {
		if got != b {
			return errors.New("not authorized")
		}
		return nil
	})
	service.loadBinding = service.loadProvider
	return service, store, p, b, allow
}
func delegationLogin(p *FederatedProvider, now time.Time, id, refresh string, ttl time.Duration) *FederatedIdentity {
	return &FederatedIdentity{Issuer: p.issuer.Issuer, Subject: "secret-subject", Nonce: "secret-nonce", ExpiresAt: now.Add(ttl), credentials: &EphemeralFederatedCredentials{idToken: id, refreshToken: refresh, receivedAt: now}}
}
func delegationRenewal(p *FederatedProvider, now time.Time, id, refresh string) *FederatedRefreshResult {
	r := &FederatedRefreshResult{Credentials: FederatedRefreshCredentials{EphemeralFederatedCredentials: EphemeralFederatedCredentials{idToken: id, refreshToken: refresh, receivedAt: now}}}
	if id != "" {
		r.Identity = &FederatedIdentity{Issuer: p.issuer.Issuer, Subject: "secret-subject", ExpiresAt: now.Add(time.Hour)}
	}
	return r
}
func delegationPlain(t *testing.T, s *DelegationService, ciphertext string) string {
	t.Helper()
	plain, err := s.decrypt(ciphertext)
	require.NoError(t, err)
	return plain
}

func TestDelegationServiceConcurrentRefresh(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	started, release := make(chan struct{}), make(chan struct{})
	var posts atomic.Int64
	s.refreshIdentity = func(_ context.Context, _ *FederatedProvider, refresh, subject, nonce string) (*FederatedRefreshResult, error) {
		posts.Add(1)
		close(started)
		<-release
		if refresh != "old-refresh" || subject != "secret-subject" || nonce != "secret-nonce" {
			return nil, ErrFederatedIdentity
		}
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	type outcome struct {
		assertion DelegationAssertion
		err       error
	}
	winner := make(chan outcome, 1)
	go func() { a, err := s.Resolve(t.Context(), b, allow); winner <- outcome{a, err} }()
	<-started
	losers := make(chan error, 16)
	for range 16 {
		go func() { _, err := s.Resolve(t.Context(), b, allow); losers <- err }()
	}
	for range 16 {
		require.ErrorIs(t, <-losers, ErrDelegationTemporary)
	}
	require.Equal(t, int64(1), posts.Load())
	close(release)
	result := <-winner
	require.NoError(t, result.err)
	require.Equal(t, "new-id", result.assertion.Value())
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, 1, store.claims)
	require.Equal(t, int64(3), c.generation)
	require.Equal(t, uuid.Nil, c.claim)
	require.Equal(t, "new-refresh", delegationPlain(t, s, c.refresh))
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "new-id", a.Value())
	require.Equal(t, int64(1), posts.Load())
}

func TestDelegationServiceClaimFailureNeverPosts(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	store.claimError = errors.New("database unavailable")
	var posts atomic.Int64
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		posts.Add(1)
		return nil, nil
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	require.Zero(t, posts.Load())
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.generation)
	require.Equal(t, uuid.Nil, c.claim)
}

func TestDelegationServiceAmbiguousNeverReplays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		failure       error
		finishFailure bool
	}{
		{name: "timeout", failure: &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}},
		{name: "unknown transport error", failure: context.DeadlineExceeded},
		{name: "nil response"},
		{name: "rotation persistence failed", finishFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			var posts int
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				if tc.finishFailure {
					return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
				}
				return nil, tc.failure
			}
			if tc.finishFailure {
				store.finishError = errors.New("write failed")
			}
			_, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationTemporary)
			later := s.now().Add(24 * time.Hour)
			s.now = func() time.Time { return later }
			_, err = s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationTemporary)
			require.Equal(t, 1, posts)
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, c.claim)
		})
	}
}

func TestDelegationServiceCallbackWinsRefresh(t *testing.T) {
	t.Parallel()
	for _, kind := range []FederatedRefreshFailure{"", FederatedRefreshInvalidGrant, FederatedRefreshInvalidIdentity} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			started, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				close(started)
				<-release
				if kind != "" {
					return nil, &FederatedRefreshError{Kind: kind}
				}
				return delegationRenewal(p, s.now(), "loser-id", "loser-refresh"), nil
			}
			go func() { _, err := s.Resolve(t.Context(), b, allow); done <- err }()
			<-started
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "winner-id", "winner-refresh", time.Hour), true))
			winner, err := store.load(t.Context(), b)
			require.NoError(t, err)
			close(release)
			require.ErrorIs(t, <-done, ErrDelegationTemporary)
			after, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, winner, after)
			a, err := s.Resolve(t.Context(), b, allow)
			require.NoError(t, err)
			require.Equal(t, "winner-id", a.Value())
			require.Equal(t, "winner-refresh", delegationPlain(t, s, after.refresh))
		})
	}
}

func TestDelegationServiceRefreshRotation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, id, refresh, wantRefresh string
		expiry                         bool
	}{
		{name: "rotation", id: "new-id", refresh: "new-refresh", wantRefresh: "new-refresh"},
		{name: "refresh omitted", id: "new-id", wantRefresh: "old-refresh"},
		{name: "ID omitted", refresh: "new-refresh", wantRefresh: "new-refresh"},
		{name: "both omitted", wantRefresh: "old-refresh"},
		{name: "known refresh expiry", id: "new-id", refresh: "new-refresh", wantRefresh: "new-refresh", expiry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				r := delegationRenewal(p, s.now(), tc.id, tc.refresh)
				if tc.expiry {
					expiry := s.now().Add(2 * time.Hour)
					r.Credentials.refreshExpiresAt = &expiry
				}
				return r, nil
			}
			a, err := s.Resolve(t.Context(), b, allow)
			if tc.id == "" {
				require.ErrorIs(t, err, ErrDelegationReauthentication)
				require.Empty(t, a.Value())
				for range 3 {
					again, err := s.Resolve(t.Context(), b, allow)
					require.ErrorIs(t, err, ErrDelegationReauthentication)
					require.Empty(t, again.Value())
				}
				require.Equal(t, 1, posts, "missing ID token must stop portable renewal, not spend more refresh grants")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.id, a.Value())
			}
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, uuid.Nil, c.claim)
			require.Equal(t, s.now(), c.refreshedAt, "successful refresh is observed even without an ID token")
			require.Equal(t, tc.wantRefresh, delegationPlain(t, s, c.refresh))
			require.Equal(t, tc.id, delegationPlain(t, s, c.assertion))
			if tc.expiry {
				require.Equal(t, s.now().Add(2*time.Hour), c.refreshExpiry)
			} else {
				require.True(t, c.refreshExpiry.IsZero())
			}
		})
	}
}

func TestDelegationServiceAssertionSafetyWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ttl  time.Duration
		want bool
	}{
		{"usable", time.Minute + time.Second, true}, {"boundary", time.Minute, false}, {"near expiry", time.Second, false}, {"expired", -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "secret-id", "", tc.ttl), false))
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				return nil, nil
			}
			a, err := s.Resolve(t.Context(), b, allow)
			if tc.want {
				require.NoError(t, err)
				require.Equal(t, "secret-id", a.Value())
			} else {
				require.ErrorIs(t, err, ErrDelegationReauthentication)
				require.Empty(t, a.Value())
			}
			require.Zero(t, posts)
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Empty(t, c.refresh)
			if tc.ttl < 0 {
				require.Empty(t, c.assertion)
			}
		})
	}
}

func TestDelegationServiceAuthorizationAndTenantIsolation(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"nil authority", "denied", "other tenant", "other human", "changed configuration"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "secret-id", "secret-refresh", time.Hour), true))
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				return nil, nil
			}
			request := b
			switch name {
			case "nil authority":
				allow = nil
			case "denied":
				allow = delegationTestAuthority(func(context.Context, DelegationBinding) error { return errors.New("denied") })
			case "other tenant":
				request.OrganizationID = "other-org"
				allow = delegationTestAuthority(func(context.Context, DelegationBinding) error { return nil })
			case "other human":
				request.HumanID = "other-human"
				allow = delegationTestAuthority(func(context.Context, DelegationBinding) error { return nil })
			case "changed configuration":
				p.client.ClientID = "replacement-client"
			}
			before, err := store.load(t.Context(), b)
			require.NoError(t, err)
			a, err := s.Resolve(t.Context(), request, allow)
			require.Error(t, err)
			require.Empty(t, a.Value())
			require.Zero(t, posts)
			after, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestDelegationServiceEncryptedRetentionAndRedaction(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	identity := delegationLogin(p, s.now(), "secret-id", "secret-refresh", time.Hour)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, identity, true))
	require.Nil(t, identity.credentials)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	for _, pair := range []struct{ encrypted, plain string }{{c.assertion, "secret-id"}, {c.refresh, "secret-refresh"}, {c.subject, "secret-subject"}, {c.nonce, "secret-nonce"}} {
		require.NotEmpty(t, pair.encrypted)
		require.NotContains(t, pair.encrypted, pair.plain)
		require.Equal(t, pair.plain, delegationPlain(t, s, pair.encrypted))
	}
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	for _, value := range []any{a, &a, c, &c} {
		require.NotContains(t, fmt.Sprintf("%v %+v %#v", value, value, value), "secret-")
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(data), "secret-")
	}
	require.True(t, c.refreshExpiry.IsZero())
}

func TestDelegationServiceRefusalAndNormalLogin(t *testing.T) {
	t.Parallel()
	s, store, p, b, _ := newDelegationUnitFixture(t)
	require.NoError(t, s.RecordOfflineRefusal(t.Context(), p, b.HumanID))
	status, err := s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.True(t, status.Refused)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "normal-id", "", time.Hour), false))
	status, err = s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.True(t, status.Refused)
	require.False(t, status.UsableRefresh)
	p.client.ClientID = "changed-client"
	status, err = s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.False(t, status.Refused)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "new-id", "new-refresh", time.Hour), true))
	before, err := store.load(t.Context(), b)
	require.NoError(t, err)
	normal := delegationLogin(p, s.now(), "normal-new-id", "", time.Hour)
	normal.Nonce = "new-login-nonce"
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, normal, false))
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, before.refresh, after.refresh)
	require.Equal(t, before.nonce, after.nonce)
	status, err = s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.True(t, status.UsableRefresh)
	require.False(t, status.Refused)
	// A refusal arriving after a successful login cannot erase durable access.
	require.NoError(t, s.RecordOfflineRefusal(t.Context(), p, b.HumanID))
	final, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, after, final)
}

func TestDelegationServiceRefreshRechecksAuthorityAndConfiguration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"authority revoked", "configuration changed", "identity changed"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			revoked := false
			if name == "authority revoked" {
				allow = delegationTestAuthority(func(context.Context, DelegationBinding) error {
					if revoked {
						return ErrDelegationReauthentication
					}
					return nil
				})
			}
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				result := delegationRenewal(p, s.now(), "new-id", "new-refresh")
				switch name {
				case "authority revoked":
					revoked = true
				case "configuration changed":
					p.client.ClientID = "changed-client"
				case "identity changed":
					result.Identity.Subject = "other-subject"
				}
				return result, nil
			}
			a, err := s.Resolve(t.Context(), b, allow)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Empty(t, a.Value())
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Empty(t, c.assertion)
			require.Empty(t, c.refresh)
			require.Empty(t, c.subject)
			require.Empty(t, c.nonce)
		})
	}
}

func TestDelegationServiceRefusalDoesNotReleaseRefreshClaim(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var posts atomic.Int64
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		if posts.Add(1) == 1 {
			close(started)
			<-release
			return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
		}
		return nil, &FederatedRefreshError{Kind: FederatedRefreshInvalidGrant}
	}
	go func() { _, err := s.Resolve(t.Context(), b, allow); done <- err }()
	<-started
	// Always unblock the original refresh, even if a regression fails assertions.
	defer func() { close(release); <-done }()
	claimed, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.NoError(t, s.RecordOfflineRefusal(t.Context(), p, b.HumanID))
	current, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, claimed.claim, current.claim, "refusal must not release rotating-token ownership")
	require.Equal(t, claimed.generation, current.generation, "refusal must not supersede the refresh winner")
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	require.Equal(t, int64(1), posts.Load())
}

func TestDelegationServicePreservedRefreshUsesOriginalNonce(t *testing.T) {
	t.Parallel()
	s, _, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "initial-id", "original-refresh", time.Hour), true))
	normal := delegationLogin(p, s.now(), "callback-id", "", 30*time.Second)
	normal.Nonce = "replacement-login-nonce"
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, normal, false))
	var gotRefresh, gotNonce string
	s.refreshIdentity = func(_ context.Context, _ *FederatedProvider, refresh, subject, nonce string) (*FederatedRefreshResult, error) {
		gotRefresh = refresh
		gotNonce = nonce
		return delegationRenewal(p, s.now(), "renewed-id", "rotated-refresh"), nil
	}
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "renewed-id", a.Value())
	require.Equal(t, "original-refresh", gotRefresh)
	require.Equal(t, "secret-nonce", gotNonce)
}

func TestDelegationServiceRefusalSurvivesSecretCleanup(t *testing.T) {
	t.Parallel()
	s, store, p, b, _ := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "", time.Hour), true))
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.False(t, c.refusedAt.IsZero())
	// Maintenance erases personal data but deliberately retains refusal metadata.
	clean := clearDelegationSecrets(c)
	ok, err := store.save(t.Context(), b, c.generation, clean)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "normal-id", "", time.Hour), false))
	status, err := s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.True(t, status.Refused)
	require.False(t, status.UsableRefresh)
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, c.refusedAt, after.refusedAt)
	require.Equal(t, c.requestConfig, after.requestConfig)
}

func TestDelegationServiceNormalLoginCannotReviveAmbiguousRefresh(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "spent-refresh", 30*time.Second), true))
	posts := 0
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		posts++
		return nil, &FederatedRefreshError{Kind: FederatedRefreshAmbiguous}
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "callback-id", "", 30*time.Second), false))
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, c.refresh)
	require.Equal(t, uuid.Nil, c.claim)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication)
	require.Equal(t, 1, posts)
}

func TestDelegationServiceRefreshScopeErrorRequiresRemediation(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	posts := 0
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		posts++
		return nil, classifyFederatedRefreshResponse(400, []byte(`{"error":"invalid_scope","error_description":"secret-provider-details"}`))
	}
	for range 2 {
		a, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrDelegationConfiguration)
		require.Empty(t, a.Value())
		require.NotContains(t, err.Error(), "secret-")
	}
	require.Equal(t, 1, posts)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.True(t, c.refusedAt.IsZero())
	require.Equal(t, "configuration_failure", c.status)
}

func TestDelegationServiceRefreshErasesAssertionExpiredBeforeWrite(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		result := delegationRenewal(p, s.now(), "new-id", "rotated-refresh")
		result.Identity.ExpiresAt = s.now().Add(2 * time.Second)
		return result, nil
	}
	initial := s.now()
	calls := 0
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error) {
		calls++
		// Model a slow live configuration/authorization read after token validation.
		if calls == 2 {
			s.now = func() time.Time { return initial.Add(3 * time.Second) }
		}
		return p, nil
	}
	a, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication)
	require.Empty(t, a.Value())
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, c.assertion, "an assertion that expired during the final live check must not be persisted")
	require.True(t, c.assertionExpiry.IsZero())
	require.Equal(t, "rotated-refresh", delegationPlain(t, s, c.refresh))
}

type delegationLoadHookStore struct {
	delegationStore
	afterLoad func()
}

func (s delegationLoadHookStore) load(ctx context.Context, b DelegationBinding) (delegationCredential, error) {
	c, err := s.delegationStore.load(ctx, b)
	s.afterLoad()
	return c, err
}

func TestDelegationServiceLoginErasesAssertionExpiredBeforeWrite(t *testing.T) {
	t.Parallel()
	s, store, p, b, _ := newDelegationUnitFixture(t)
	initial := s.now()
	s.store = delegationLoadHookStore{delegationStore: store, afterLoad: func() { s.now = func() time.Time { return initial.Add(3 * time.Second) } }}
	identity := delegationLogin(p, initial, "expiring-id", "fresh-refresh", time.Second)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, identity, true))
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, c.assertion, "callback must recheck expiry after reading the stored generation")
	require.True(t, c.assertionExpiry.IsZero())
	require.Equal(t, "fresh-refresh", delegationPlain(t, s, c.refresh))
}

func TestDelegationServiceInvalidGrantPreservesUsableAssertion(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "valid-id", "invalid-refresh", time.Hour), true))
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	claim := uuid.New()
	ok, err := store.claim(t.Context(), b, c.generation, claim, s.now())
	require.NoError(t, err)
	require.True(t, ok)
	claimed, err := store.load(t.Context(), b)
	require.NoError(t, err)
	err = s.recordRefreshFailure(t.Context(), b, claimed, claim, &FederatedRefreshError{Kind: FederatedRefreshInvalidGrant})
	require.ErrorIs(t, err, ErrDelegationReauthentication)
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, after.refresh)
	require.Equal(t, "reauthentication_required", after.status)
	require.Equal(t, c.assertion, after.assertion)
	require.Equal(t, c.subject, after.subject)
	posts := 0
	s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
		posts++
		return nil, nil
	}
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "valid-id", a.Value())
	later := s.now().Add(time.Hour)
	s.now = func() time.Time { return later }
	a, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationReauthentication)
	require.Empty(t, a.Value())
	require.Zero(t, posts)
}

func TestDelegationServiceRejectsSubjectRebinding(t *testing.T) {
	t.Parallel()
	s, store, p, b, _ := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", time.Hour), true))
	before, err := store.load(t.Context(), b)
	require.NoError(t, err)
	login := delegationLogin(p, s.now(), "other-id", "other-refresh", time.Hour)
	login.Subject = "other-subject"
	require.ErrorIs(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true), ErrDelegationConfiguration)
	after, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestDelegationServiceValidAssertionAvoidsDiscovery(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "valid-id", "refresh", time.Hour), true))
	s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error) {
		t.Error("valid assertion must not load discovery")
		return nil, errors.New("discovery unavailable")
	}
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "valid-id", a.Value())
	require.Zero(t, store.claims)
}

func TestDelegationServicePostRefreshVerification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                              string
		providerFailure, authorityFailure error
		changed                           bool
		definitive                        bool
	}{
		{name: "provider transient", providerFailure: context.DeadlineExceeded},
		{name: "authority transient", authorityFailure: errors.New("authority unavailable")},
		{name: "provider revoked", providerFailure: ErrDelegationConfiguration, definitive: true},
		{name: "authority revoked", authorityFailure: ErrDelegationReauthentication, definitive: true},
		{name: "configuration changed", changed: true, definitive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, _ := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			renewed := false
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				renewed = true
				return delegationRenewal(p, s.now(), "new-id", "rotation"), nil
			}
			s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error) {
				if renewed {
					if tc.providerFailure != nil {
						return nil, tc.providerFailure
					}
					if tc.changed {
						changed := *p
						changed.client.ClientID = "changed-client"
						return &changed, nil
					}
				}
				return p, nil
			}
			authority := delegationTestAuthority(func(context.Context, DelegationBinding) error {
				if renewed {
					return tc.authorityFailure
				}
				return nil
			})
			a, err := s.Resolve(t.Context(), b, authority)
			require.Empty(t, a.Value())
			if tc.definitive {
				require.ErrorIs(t, err, ErrDelegationConfiguration)
			} else {
				require.ErrorIs(t, err, ErrDelegationTemporary)
			}
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, s.now(), c.refreshedAt)
			if tc.definitive {
				require.Empty(t, c.refresh)
				require.Empty(t, c.assertion)
				require.Equal(t, uuid.Nil, c.claim)
			} else {
				require.Equal(t, "rotation", delegationPlain(t, s, c.refresh))
				require.NotEqual(t, uuid.Nil, c.claim)
				// Even after dependencies recover, a quarantined assertion is not released.
				renewed = false
				_, err = s.Resolve(t.Context(), b, authority)
				require.ErrorIs(t, err, ErrDelegationTemporary)
				require.Equal(t, 1, store.claims)
			}
		})
	}
}

func TestDelegationServiceOfflineStatusRejectsNonresolvableCredentials(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"configuration_failure", "reauthentication_required", "refused", "unknown"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, _ := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
			c := store.rows[b]
			c.status = status
			store.rows[b] = c
			result, err := s.OfflineStatus(t.Context(), p, b.HumanID)
			require.NoError(t, err)
			require.False(t, result.UsableRefresh)
		})
	}
}

func TestDelegationServiceRevokeAfterTrustRemoval(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
	store.trustRemoved = true
	s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error) {
		t.Fatal("revocation must not require live trust")
		return nil, ErrDelegationConfiguration
	}
	require.ErrorIs(t, s.Revoke(t.Context(), b, nil), ErrDelegationConfiguration)
	require.NotEmpty(t, store.rows[b].refresh)
	require.NoError(t, s.Revoke(t.Context(), b, allow))
	c := store.rows[b]
	require.Empty(t, c.refresh)
	require.Empty(t, c.assertion)
	require.Empty(t, c.subject)
	require.Empty(t, c.nonce)
	require.Equal(t, int64(2), c.generation)
}

func TestDelegationServicePrePOSTAttempt(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"decrypt failure", "CAS conflict", "database failure", "success"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", 30*time.Second), true))
			switch name {
			case "decrypt failure":
				c := store.rows[b]
				c.refresh = "invalid ciphertext"
				store.rows[b] = c
			case "CAS conflict":
				store.attemptConflict = true
			case "database failure":
				store.attemptError = errors.New("database unavailable")
			}
			posts := 0
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				posts++
				require.Equal(t, s.now(), store.attemptedAt)
				return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
			}
			_, err := s.Resolve(t.Context(), b, allow)
			switch name {
			case "success":
				require.NoError(t, err)
				require.Equal(t, 1, posts)
			case "decrypt failure":
				require.ErrorIs(t, err, ErrDelegationConfiguration)
			default:
				require.ErrorIs(t, err, ErrDelegationTemporary)
			}
			if name != "success" {
				require.Zero(t, posts)
				require.True(t, store.attemptedAt.IsZero())
				require.Equal(t, uuid.Nil, store.rows[b].claim)
			}
			if name == "database failure" || name == "CAS conflict" {
				store.attemptError = nil
				store.attemptConflict = false
				_, err = s.Resolve(t.Context(), b, allow)
				require.NoError(t, err)
				require.Equal(t, 1, posts)
			}
		})
	}
}

func TestDelegationServiceLoadsProviderAfterClaim(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fresh provider", "changed registration", "deleted", "invalid", "temporary"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", 30*time.Second), true))
			fresh := *p
			fresh.metadata.TokenEndpoint = "https://issuer.example/new-token"
			want := ErrDelegationConfiguration
			s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (*FederatedProvider, error) {
				require.NotEqual(t, uuid.Nil, store.rows[b].claim, "full provider must be loaded after claim")
				switch name {
				case "changed registration":
					fresh.client.ClientID = "replacement-client"
				case "deleted":
					return nil, pgx.ErrNoRows
				case "invalid":
					return nil, ErrFederatedConfiguration
				case "temporary":
					return nil, errors.New("discovery unavailable")
				}
				return &fresh, nil
			}
			posts := 0
			s.refreshIdentity = func(_ context.Context, got *FederatedProvider, _, _, _ string) (*FederatedRefreshResult, error) {
				posts++
				require.Same(t, &fresh, got)
				return delegationRenewal(p, s.now(), "new-id", "rotation"), nil
			}
			_, err := s.Resolve(t.Context(), b, allow)
			if name == "fresh provider" {
				require.NoError(t, err)
				require.Equal(t, 1, posts)
			} else {
				if name == "temporary" {
					want = ErrDelegationTemporary
				}
				require.ErrorIs(t, err, want)
				require.Zero(t, posts)
			}
			require.Equal(t, uuid.Nil, store.rows[b].claim)
		})
	}
}

func TestDelegationServiceInitialAuthorizationErrors(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{ErrDelegationTemporary, errors.New("dependency unavailable"), ErrDelegationConfiguration, ErrDelegationReauthentication} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			s, store, p, b, _ := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
			before := store.rows[b]
			a, err := s.Resolve(t.Context(), b, delegationTestAuthority(func(context.Context, DelegationBinding) error {
				return fmt.Errorf("authorization: %w", cause)
			}))
			want := ErrDelegationTemporary
			if definitiveDelegationFailure(cause) {
				want = ErrDelegationConfiguration
			}
			require.ErrorIs(t, err, want)
			require.Empty(t, a.Value())
			require.Equal(t, before, store.rows[b])
		})
	}
}

func TestDelegationServiceOfflineStatusInvalidPolicy(t *testing.T) {
	t.Parallel()
	for _, retained := range []bool{false, true} {
		t.Run(fmt.Sprint(retained), func(t *testing.T) {
			t.Parallel()
			s, _, p, b, _ := newDelegationUnitFixture(t)
			if retained {
				require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
			}
			p.client.Scope = []string{"openid"}
			require.Empty(t, p.OfflineConfigurationHash())
			status, err := s.OfflineStatus(t.Context(), p, b.HumanID)
			require.ErrorIs(t, err, ErrDelegationConfiguration)
			require.Equal(t, DelegationOfflineStatus{}, status)
		})
	}
}

type delegationAttemptHookStore struct {
	delegationStore
	beforeAttempt    func()
	finishContextErr error
}

func (s *delegationAttemptHookStore) markRefreshAttempt(ctx context.Context, b DelegationBinding, generation int64, claim uuid.UUID, now time.Time) (bool, error) {
	s.beforeAttempt()
	return s.delegationStore.markRefreshAttempt(ctx, b, generation, claim, now)
}

func (s *delegationAttemptHookStore) finish(ctx context.Context, b DelegationBinding, generation int64, claim uuid.UUID, next delegationCredential) (bool, error) {
	s.finishContextErr = ctx.Err()
	return s.delegationStore.finish(ctx, b, generation, claim, next)
}

func TestDelegationServiceAttemptFailureReleaseSafety(t *testing.T) {
	t.Parallel()
	for _, superseded := range []bool{false, true} {
		t.Run(fmt.Sprint(superseded), func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", 30*time.Second), true))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var callback delegationCredential
			hook := &delegationAttemptHookStore{delegationStore: store}
			hook.beforeAttempt = func() {
				if superseded {
					require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "callback-id", "callback-refresh", time.Hour), true))
					callback = store.rows[b]
				} else {
					store.attemptError = errors.New("database unavailable")
				}
				cancel()
			}
			s.store = hook
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				t.Fatal("failed attempt must not POST")
				return nil, nil
			}
			_, err := s.Resolve(ctx, b, allow)
			require.ErrorIs(t, err, ErrDelegationTemporary)
			require.NoError(t, hook.finishContextErr, "claim release must survive caller cancellation")
			require.Equal(t, uuid.Nil, store.rows[b].claim)
			if superseded {
				require.Equal(t, callback, store.rows[b], "release must not overwrite a newer callback")
			} else {
				require.Equal(t, "refresh", delegationPlain(t, s, store.rows[b].refresh))
			}
		})
	}
}
