package delegation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestRevokeAuthorizationFailureClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		cause, want error
	}{
		{"dependency outage", errors.New("authorization dependency unavailable"), ErrTemporary},
		{"canceled", context.Canceled, ErrTemporary},
		{"deadline", context.DeadlineExceeded, ErrTemporary},
		{"configuration", ErrConfiguration, ErrConfiguration},
		{"reauthentication", ErrReauthentication, ErrConfiguration},
		{"missing binding", pgx.ErrNoRows, ErrConfiguration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "refresh", time.Hour), true))
			before := store.rows[b]
			denied := delegationTestAuthority(func(context.Context, Binding) error { return fmt.Errorf("authorize: %w", tc.cause) })
			require.ErrorIs(t, s.Revoke(t.Context(), b, denied), tc.want)
			require.Equal(t, before, store.rows[b], "authorization failure must not mutate retained state")
			require.NoError(t, s.Revoke(t.Context(), b, allow), "retry after authorization recovers must erase credentials")
			after := store.rows[b]
			require.Empty(t, after.assertion)
			require.Empty(t, after.refresh)
			require.Empty(t, after.subject)
			require.Empty(t, after.nonce)
			require.Greater(t, after.generation, before.generation)
		})
	}
}

func TestLoginDistinguishesAbsentAndZeroRefreshExpiry(t *testing.T) {
	t.Parallel()
	for _, retained := range []bool{false, true} {
		for _, rotate := range []bool{false, true} {
			for _, explicitZero := range []bool{false, true} {
				t.Run(fmt.Sprintf("retained=%t/rotate=%t/zero=%t", retained, rotate, explicitZero), func(t *testing.T) {
					t.Parallel()
					s, store, p, b, _ := newDelegationUnitFixture(t)
					originalExpiry := s.now().Add(2 * time.Hour)
					if retained {
						login := delegationLogin(p, s.now(), "old-id", "old-refresh", time.Hour)
						login.credentials.value.refreshExpiresAt = &originalExpiry
						require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
					}
					token := ""
					if rotate {
						token = "new-refresh"
					}
					login := delegationLogin(p, s.now(), "new-id", token, time.Hour)
					if explicitZero {
						zero := time.Time{}
						login.credentials.value.refreshExpiresAt = &zero
					}
					require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
					c := store.rows[b]
					require.Equal(t, "new-id", delegationPlain(t, s, c.assertion))
					switch {
					case explicitZero || (!retained && !rotate):
						require.Empty(t, c.refresh)
						require.True(t, c.refreshExpiry.IsZero())
					case rotate:
						require.Equal(t, "new-refresh", delegationPlain(t, s, c.refresh))
						require.True(t, c.refreshExpiry.IsZero(), "omitted lifetime on a newly issued token remains unknown")
					default:
						require.Equal(t, "old-refresh", delegationPlain(t, s, c.refresh))
						require.Equal(t, originalExpiry, c.refreshExpiry, "omission preserves the previous credential and bound")
					}
				})
			}
		}
	}
}

func TestRefreshDistinguishesAbsentAndZeroExpiry(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		for _, rotate := range []bool{false, true} {
			for _, explicitZero := range []bool{false, true} {
				t.Run(fmt.Sprintf("known=%t/rotate=%t/zero=%t", known, rotate, explicitZero), func(t *testing.T) {
					t.Parallel()
					s, store, p, b, allow := newDelegationUnitFixture(t)
					originalExpiry := time.Time{}
					login := delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second)
					if known {
						originalExpiry = s.now().Add(2 * time.Hour)
						login.credentials.value.refreshExpiresAt = &originalExpiry
					}
					require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
					s.refreshIdentity = func(context.Context, Provider, string, string, string) (*RefreshResult, error) {
						token := ""
						if rotate {
							token = "new-refresh"
						}
						result := delegationRenewal(p, s.now(), "new-id", token)
						credentials, ok := result.Credentials.(*FederatedRefreshCredentials)
						require.True(t, ok)
						if explicitZero {
							zero := time.Time{}
							credentials.refreshExpiresAt = &zero
						}
						return result, nil
					}
					assertion, err := s.Resolve(t.Context(), b, allow)
					require.NoError(t, err)
					require.Equal(t, "new-id", assertion.Value())
					c := store.rows[b]
					if explicitZero {
						require.Empty(t, c.refresh)
						require.True(t, c.refreshExpiry.IsZero())
					} else {
						token := "old-refresh"
						if rotate {
							token = "new-refresh"
						}
						require.Equal(t, token, delegationPlain(t, s, c.refresh))
						require.Equal(t, originalExpiry, c.refreshExpiry, "omission must not remove an existing bound")
					}
				})
			}
		}
	}
}

func TestLoginRefreshLifetimeFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		seconds     int64
		receivedAgo time.Duration
		absolute    string
		wantExpiry  time.Duration
		expired     bool
	}{
		{name: "relative lifetime", seconds: 120, receivedAgo: 30 * time.Second, wantExpiry: 90 * time.Second},
		{name: "relative lifetime already expired", seconds: 30, receivedAgo: time.Minute, expired: true},
		{name: "zero relative lifetime is unknown"},
		{name: "negative relative lifetime is unknown", seconds: -1},
		{name: "overflow relative lifetime is unknown", seconds: 1<<63 - 1},
		{name: "absolute lifetime takes precedence", seconds: 120, absolute: "future", wantExpiry: time.Hour},
		{name: "explicit zero overrides relative lifetime", seconds: 120, absolute: "zero", expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, _ := newDelegationUnitFixture(t)
			login := delegationLogin(p, s.now(), "id", "refresh", time.Hour)
			login.credentials.value.receivedAt = s.now().Add(-tc.receivedAgo)
			login.credentials.value.refreshExpiresIn = tc.seconds
			switch tc.absolute {
			case "future":
				expiry := s.now().Add(time.Hour)
				login.credentials.value.refreshExpiresAt = &expiry
			case "zero":
				zero := time.Time{}
				login.credentials.value.refreshExpiresAt = &zero
			}
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
			c := store.rows[b]
			if tc.expired {
				require.Empty(t, c.refresh)
			} else {
				require.Equal(t, "refresh", delegationPlain(t, s, c.refresh))
			}
			if tc.wantExpiry == 0 {
				require.True(t, c.refreshExpiry.IsZero())
			} else {
				require.Equal(t, s.now().Add(tc.wantExpiry), c.refreshExpiry)
			}
		})
	}
}

func TestLoginOfflineRequestAndSupportAreIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                 string
		requested, supported bool
		want                 string
	}{
		{"not requested", false, true, "offline_not_requested"},
		{"neither requested nor supported", false, false, "offline_not_requested"},
		{"requested but unsupported", true, false, "offline_unsupported"},
		{"supported but not attempted", true, true, "assertion_only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, _ := newDelegationUnitFixture(t)
			p.offlineSupported = tc.supported
			if !tc.requested {
				p.client.Scope = []string{"openid", "email"}
			}
			require.Equal(t, tc.requested, p.OfflineRequested())
			require.Equal(t, tc.supported, p.OfflineSupported())
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "id", "", time.Hour), false))
			c := store.rows[b]
			require.Equal(t, tc.want, c.status)
			require.Equal(t, "id", delegationPlain(t, s, c.assertion))
			require.Empty(t, c.refresh)
			require.True(t, c.refusedAt.IsZero(), "no consent was attempted")
		})
	}
}
