package delegation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A renewal that succeeded but couldn't recheck authority must leave a kept row closed past its pause.
func TestDelegationConfigurationFailureQuarantineKeepsStatus(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := failedConfigurationFixture(t)
	failed, err := store.load(t.Context(), b)
	require.NoError(t, err)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	calls := 0
	flaky := delegationTestAuthority(func(context.Context, Binding) error {
		calls++
		if calls > 1 {
			return errors.New("authority read failed")
		}
		return nil
	})
	delegationMoveClock(s, configurationRetryDelay)
	_, err = s.Resolve(t.Context(), b, flaky)
	require.ErrorIs(t, err, ErrTemporary)

	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", c.status, "stored downstream tokens must stay closed")
	require.NotEqual(t, uuid.Nil, c.claim)
	require.Equal(t, failed.retryAfter, c.retryAfter)

	delegationMoveClock(s, time.Hour)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	require.NoError(t, s.Check(t.Context(), b, allow), "the pause is over; the status keeps stored tokens closed")
	c, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", c.status, "stored downstream tokens stay closed past the pause")
}

func TestDelegationClaimInFlightThenAbandoned(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return nil, &RefreshError{Kind: RefreshAmbiguous}
	}
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)

	delegationMoveClock(s, refreshClaimLimit-time.Second)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.ErrorIs(t, s.CheckResolvable(t.Context(), b, allow), ErrTemporary)

	delegationMoveClock(s, time.Second)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	require.ErrorIs(t, s.CheckResolvable(t.Context(), b, allow), ErrReauthentication)

	status, err := s.OfflineStatus(t.Context(), p, b.HumanID)
	require.NoError(t, err)
	require.False(t, status.UsableRefresh, "the sign-in that clears the claim asks for a new refresh token")
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "new-id", "new-refresh", time.Hour), true))
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "new-id", a.Value())
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, c.claim)
}

// A claim taken but not yet stamped as an attempt is still a renewal in flight.
func TestDelegationClaimBeforeTheAttemptIsInFlight(t *testing.T) {
	t.Parallel()
	s, _, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
	entered, proceed := make(chan struct{}), make(chan struct{})
	load := s.loadProvider
	s.loadProvider = func(ctx context.Context, org string, issuer, client uuid.UUID) (Provider, error) {
		close(entered)
		<-proceed
		return load(ctx, org, issuer, client)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Resolve(t.Context(), b, allow)
		done <- err
	}()
	<-entered
	_, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.ErrorIs(t, s.CheckResolvable(t.Context(), b, allow), ErrTemporary)
	close(proceed)
	require.Error(t, <-done)
}

func TestDelegationStaleRegistrationNeedsSignIn(t *testing.T) {
	t.Parallel()
	t.Run("before the claim", func(t *testing.T) {
		t.Parallel()
		s, store, p, b, allow := newDelegationUnitFixture(t)
		require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", time.Hour), true))
		store.mu.Lock()
		c := store.rows[b]
		c.config = "earlier-registration"
		store.rows[b] = c
		store.mu.Unlock()
		_, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrStaleRegistration)
	})
	t.Run("after the claim", func(t *testing.T) {
		t.Parallel()
		s, store, p, b, allow := newDelegationUnitFixture(t)
		require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
		s.loadProvider = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
			changed := *p
			changed.client.ClientID = "changed-client"
			return &changed, nil
		}
		_, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrStaleRegistration)
		c, err := store.load(t.Context(), b)
		require.NoError(t, err)
		require.Equal(t, uuid.Nil, c.claim, "no request started, so the claim is released")
	})
	t.Run("during the renewal", func(t *testing.T) {
		t.Parallel()
		s, _, p, b, allow := newDelegationUnitFixture(t)
		require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
		s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
			s.loadBinding = func(context.Context, string, uuid.UUID, uuid.UUID) (Provider, error) {
				changed := *p
				changed.client.ClientID = "changed-client"
				return &changed, nil
			}
			return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
		}
		_, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrStaleRegistration)
	})
}

// Only a row kept on purpose accepts a different subject.
func TestDelegationSubjectPinOnFailedRows(t *testing.T) {
	t.Parallel()
	otherLogin := func(p Provider, s *Service) *FederatedIdentity {
		login := delegationLogin(p, s.now(), "other-id", "other-refresh", time.Hour)
		login.Subject = "other-subject"
		return login
	}
	t.Run("failed row not kept on purpose stays pinned", func(t *testing.T) {
		t.Parallel()
		s, store, p, b, _ := newDelegationUnitFixture(t)
		require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", time.Hour), true))
		store.mu.Lock()
		c := store.rows[b]
		c.status = "configuration_failure"
		c.retryAfter = time.Time{}
		store.rows[b] = c
		store.mu.Unlock()
		require.ErrorIs(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, otherLogin(p, s), true), ErrConfiguration)
	})
	t.Run("row erased for an invalid identity has no subject to pin", func(t *testing.T) {
		t.Parallel()
		s, store, p, b, allow := failedConfigurationFixture(t)
		s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
			return nil, &RefreshError{Kind: RefreshInvalidIdentity}
		}
		delegationMoveClock(s, configurationRetryDelay)
		_, err := s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrConfiguration)
		c, err := store.load(t.Context(), b)
		require.NoError(t, err)
		require.Empty(t, c.subject)
		require.False(t, configurationFailureRetained(c))
		require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, otherLogin(p, s), true))
	})
}
