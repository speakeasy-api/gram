package remotesessions

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDelegationGatewayTimeoutAfterRotationNeverResubmits(t *testing.T) {
	t.Parallel()
	s, store, p, b, allow := newDelegationUnitFixture(t)
	require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, delegationLogin(p, s.now(), "old-id", "submitted-refresh", 30*time.Second), true))
	posts := 0
	upstreamRotated := false
	doer := federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		posts++
		upstreamRotated = true // Issuer commits rotation, but gateway loses its response.
		return &http.Response{StatusCode: http.StatusGatewayTimeout, Body: io.NopCloser(strings.NewReader("gateway timeout"))}, nil
	})
	s.refreshIdentity = func(ctx context.Context, _ *FederatedProvider, token, _, _ string) (*FederatedRefreshResult, error) {
		require.Equal(t, "submitted-refresh", token)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://idp.example.test/token", nil)
		require.NoError(t, err)
		_, _, err = postFederatedRefresh(doer, req)
		return nil, err
	}
	assertion, err := s.Resolve(t.Context(), b, allow)
	require.ErrorIs(t, err, ErrDelegationTemporary)
	require.Empty(t, assertion.Value())
	require.True(t, upstreamRotated)
	later := s.now().Add(24 * time.Hour)
	s.now = func() time.Time { return later }
	for range 3 {
		assertion, err = s.Resolve(t.Context(), b, allow)
		require.ErrorIs(t, err, ErrDelegationTemporary)
		require.Empty(t, assertion.Value())
	}
	require.Equal(t, 1, posts)
	credential, err := store.load(t.Context(), b)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, credential.claim)
	require.Equal(t, "submitted-refresh", delegationPlain(t, s, credential.refresh))
	require.True(t, credential.refusedAt.IsZero())
}

func TestDelegationRefreshLifetimeWithoutRotation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		reported bool
		seconds  int64
		rotate   bool
	}{
		{name: "finite", reported: true, seconds: 120},
		{name: "zero", reported: true},
		{name: "omitted"},
		{name: "rotated omitted", rotate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			original := s.now().Add(2 * time.Hour)
			login := delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second)
			login.credentials.refreshExpiresAt = &original
			require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
			s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
				rotation := ""
				if tc.rotate {
					rotation = "new-refresh"
				}
				result := delegationRenewal(p, s.now(), "new-id", rotation)
				if tc.reported {
					expiry := s.now().Add(time.Duration(tc.seconds) * time.Second)
					result.Credentials.refreshExpiresAt = &expiry
				}
				return result, nil
			}
			assertion, err := s.Resolve(t.Context(), b, allow)
			require.NoError(t, err)
			require.Equal(t, "new-id", assertion.Value())
			credential, err := store.load(t.Context(), b)
			require.NoError(t, err)
			if tc.reported && tc.seconds == 0 {
				require.Empty(t, credential.refresh)
				require.True(t, credential.refreshExpiry.IsZero())
				require.Equal(t, "assertion_only", credential.status)
			} else {
				token := "old-refresh"
				if tc.rotate {
					token = "new-refresh"
				}
				require.Equal(t, token, delegationPlain(t, s, credential.refresh))
				want := original
				if tc.reported {
					want = s.now().Add(time.Duration(tc.seconds) * time.Second)
				}
				require.Equal(t, want, credential.refreshExpiry)
			}
		})
	}
}
