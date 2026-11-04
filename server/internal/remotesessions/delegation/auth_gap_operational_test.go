package delegation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The HTTP classifier is exercised separately in remotesessions. These tests
// inject its current classification at the dependency boundary, then execute
// the real Service.Resolve state machine and inspect its encrypted memory store.
// They do not execute HTTP, SQL, a scheduler, or a browser login.
func TestAuthOperationalGapG11UnexpectedRejectionPreservesGrant(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"WAF403", "400_invalid_client", "404_moved_endpoint"} {
		t.Run(name, func(t *testing.T) {
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second), true))
			before, err := store.load(t.Context(), b)
			require.NoError(t, err)
			calls := 0
			s.refreshIdentity = func(_ context.Context, _ Provider, refresh, subject, nonce string) (*RefreshResult, error) {
				calls++
				require.Equal(t, "old-refresh", refresh)
				require.Equal(t, "secret-subject", subject)
				require.Equal(t, "secret-nonce", nonce)
				// All three HTTP responses currently map to this classification. This
				// input must stay aligned with the separate parent-package classifier test.
				return nil, &RefreshError{Kind: RefreshConfiguration}
			}
			assertion, err := s.Resolve(t.Context(), b, allow)
			require.Error(t, err)
			require.Empty(t, assertion.Value())
			require.Equal(t, 1, calls)
			after, err := store.load(t.Context(), b)
			require.NoError(t, err)
			require.Greater(t, after.generation, before.generation, "Resolve must have written its outcome")
			require.True(t, before.refresh == after.refresh, "unexpected rejection must preserve the encrypted grant, not clear secrets")
			require.Equal(t, "old-refresh", delegationPlain(t, s, after.refresh))
			require.True(t, after.refusedAt.IsZero())
		})
	}
}

func TestAuthOperationalGapG11Retryable403ResolveRecovery(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	now := s.now()
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, now, "old-id", "old-refresh", 30*time.Second), true))
	before, err := store.load(t.Context(), b)
	require.NoError(t, err)
	calls := 0
	s.refreshIdentity = func(_ context.Context, _ Provider, refresh, subject, nonce string) (*RefreshResult, error) {
		calls++
		require.Equal(t, "old-refresh", refresh)
		require.Equal(t, "secret-subject", subject)
		require.Equal(t, "secret-nonce", nonce)
		if calls == 1 {
			// Parent-package test proves 403 temporarily_unavailable maps to retryable.
			return nil, &RefreshError{Kind: RefreshRetryable}
		}
		return delegationRenewal(p, s.now(), "recovered-id", "rotated-refresh"), nil
	}
	assertion, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.Empty(t, assertion.Value())
	failed, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, before.refresh, failed.refresh)
	require.Equal(t, "old-refresh", delegationPlain(t, s, failed.refresh))
	require.Equal(t, uuid.Nil, failed.claim)
	require.Equal(t, "temporary_failure", failed.status)
	require.Equal(t, now.Add(time.Minute), failed.retryAfter)
	require.True(t, failed.refusedAt.IsZero())
	_, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrTemporary)
	require.Equal(t, 1, calls, "backoff must prevent immediate replay")
	later := failed.retryAfter.Add(time.Second)
	s.now = func() time.Time { return later }
	assertion, err = s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "recovered-id", assertion.Value())
	require.Equal(t, 2, calls)
	recovered, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh", delegationPlain(t, s, recovered.refresh))
	require.Equal(t, "recovered-id", delegationPlain(t, s, recovered.assertion))
	require.Equal(t, uuid.Nil, recovered.claim)
	require.True(t, recovered.retryAfter.IsZero())
	require.Greater(t, recovered.generation, failed.generation)
	assertion, err = s.Resolve(t.Context(), b, allow)
	require.NoError(t, err)
	require.Equal(t, "recovered-id", assertion.Value())
	require.Equal(t, 2, calls, "recovered assertion must be reused without another refresh")
}

func TestAuthOperationalGapG11InvalidGrantResolveNegativeControl(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "revoked-refresh", 30*time.Second), true))
	calls := 0
	s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
		calls++
		return nil, &RefreshError{Kind: RefreshInvalidGrant}
	}
	assertion, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	require.Empty(t, assertion.Value())
	revoked, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.Empty(t, revoked.refresh)
	require.True(t, revoked.refreshExpiry.IsZero())
	require.Equal(t, "reauthentication_required", revoked.status)
	later := s.now().Add(24 * time.Hour)
	s.now = func() time.Time { return later }
	assertion, err = s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrReauthentication)
	require.Empty(t, assertion.Value())
	require.Equal(t, 1, calls, "fatal invalid_grant must not be retried after aging")
}
