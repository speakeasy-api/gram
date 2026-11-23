package delegation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// failRepeatedly refuses every renewal with kind and moves through the retries up to elapsed.
func failRepeatedly(t *testing.T, s *Service, b Binding, allow Authorizer, kind RefreshFailure, elapsed time.Duration) {
	t.Helper()
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return nil, &RefreshError{Kind: kind}
	}
	for moved := time.Duration(0); moved < elapsed; moved += 6 * time.Hour {
		delegationMoveClock(s, 6*time.Hour)
		_, err := s.Resolve(t.Context(), b, allow)
		if moved+6*time.Hour < configurationRetryLimit || kind == RefreshClientRejected || kind == RefreshSetup || kind == RefreshSetupRetryable {
			require.ErrorIs(t, err, ErrConfiguration)
		}
	}
}

func TestDelegationRetryLimitMovesAKeptTokenToSignIn(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow := failedConfigurationFixture(t)
	failRepeatedly(t, s, b, allow, RefreshConfiguration, configurationRetryLimit-6*time.Hour)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", c.status)
	require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh))

	delegationMoveClock(s, 6*time.Hour)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	c, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "reauthentication_required", c.status)
	require.Empty(t, c.refresh)
	require.Equal(t, s.now(), c.observedAt)

	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication, "the answer stays sign-in")
}

func TestDelegationRetryLimitStillTriesOnceAfterAnIdleStretch(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := failedConfigurationFixture(t)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	delegationMoveClock(s, 30*24*time.Hour)
	a, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err, "an administrator fixed it while this person was away")
	require.Equal(t, "new-id", a.Value())
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "new-refresh", delegationPlain(t, s, c.refresh))
}

func TestDelegationRetryLimitIgnoresClientRejections(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow := failedConfigurationFixture(t)
	failRepeatedly(t, s, b, allow, RefreshClientRejected, 2*configurationRetryLimit)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "configuration_failure", c.status)
	require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh), "a client rejection says nothing about the token, and a sign-in wouldn't fix it")

	// The clock for token-shaped refusals starts when the client stops being rejected.
	failRepeatedly(t, s, b, allow, RefreshConfiguration, 6*time.Hour)
	c, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh))
}

func TestDelegationRetryLimitStartsAfterClientOrSetupRecovery(t *testing.T) {
	t.Parallel()
	for _, initial := range []RefreshFailure{RefreshClientRejected, RefreshSetup} {
		for _, refusal := range []RefreshFailure{RefreshConfiguration, RefreshRetryable} {
			t.Run(string(initial)+"/"+string(refusal), func(t *testing.T) {
				t.Parallel()
				s, store, p, b, allow := newDelegationUnitFixture(t)
				require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
				s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
					return nil, &RefreshError{Kind: initial}
				}
				_, err := s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrRetainedConfiguration)
				require.True(t, store.rows[b].observedAt.IsZero())
				delegationMoveClock(s, configurationRetryLimit+24*time.Hour)
				s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
					return nil, &RefreshError{Kind: refusal}
				}
				_, err = s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrRetainedConfiguration)
				require.Equal(t, "old-refresh", delegationPlain(t, s, store.rows[b].refresh))
				require.Equal(t, s.now(), store.rows[b].observedAt)
				delegationMoveClock(s, configurationRetryLimit)
				_, err = s.Resolve(t.Context(), b, allow)
				require.ErrorIs(t, err, ErrReauthentication)
				require.Empty(t, store.rows[b].refresh)
			})
		}
	}
}

func TestDelegationRetryLimitCountsThroughTemporaryFailures(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow := failedConfigurationFixture(t)
	first, err := store.load(t.Context(), b)
	require.NoError(t, err)
	failRepeatedly(t, s, b, allow, RefreshRetryable, 12*time.Hour)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, first.observedAt, c.observedAt, "a 429 or temporarily_unavailable must not push the limit back")
}

func TestDelegationRetryLimitCoversRetryableRefusals(t *testing.T) {
	t.Parallel()
	s, store, _, b, allow := failedConfigurationFixture(t)
	failRepeatedly(t, s, b, allow, RefreshRetryable, configurationRetryLimit-6*time.Hour)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh))

	delegationMoveClock(s, 6*time.Hour)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication, "a kept token answered only with 429s still reaches the limit")
	c, err = store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "reauthentication_required", c.status)
	require.Empty(t, c.refresh)
}

func TestDelegationRetryLimitIgnoresLocalSetupFaults(t *testing.T) {
	t.Parallel()
	for _, kind := range []RefreshFailure{RefreshSetup, RefreshSetupRetryable} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			s, store, _, b, allow := failedConfigurationFixture(t)
			failRepeatedly(t, s, b, allow, kind, 2*configurationRetryLimit)
			c, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Equal(t, "configuration_failure", c.status)
			require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh), "no request was sent, so the provider has said nothing about the token")
		})
	}
}

func TestDelegationRetryLimitRestartsAfterARecovery(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := failedConfigurationFixture(t)
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return delegationRenewal(p, s.now(), "new-id", "new-refresh"), nil
	}
	delegationMoveClock(s, configurationRetryLimit)
	_, err := s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)

	// A later refusal starts a new limit rather than inheriting the old one.
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		return nil, &RefreshError{Kind: RefreshConfiguration}
	}
	delegationMoveClock(s, 2*time.Hour)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrConfiguration)
	delegationMoveClock(s, configurationRetryDelay)
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrConfiguration)
	c, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "new-refresh", delegationPlain(t, s, c.refresh))
}
