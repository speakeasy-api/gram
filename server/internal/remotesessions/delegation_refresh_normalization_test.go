package remotesessions

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDelegationRefreshNormalizesProviderLifetime(t *testing.T) {
	t.Parallel()
	for _, known := range []bool{false, true} {
		for _, rotate := range []bool{false, true} {
			for _, tc := range []struct {
				name    string
				field   string
				seconds int64
			}{
				{name: "alias", field: `"refresh_expires_in":120`, seconds: 120},
				{name: "standard takes precedence", field: `"refresh_expires_in":120,"refresh_token_timeout":60`, seconds: 60},
				{name: "authorization bound", field: `"refresh_expires_in":120,"authorization_expires_in":30`, seconds: 30},
				{name: "overflow ignored", field: `"refresh_expires_in":9223372036854775807`},
				{name: "negative ignored", field: `"refresh_expires_in":-1`},
				{name: "zero alias ignored", field: `"refresh_expires_in":0`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					s, store, p, b, allow := newDelegationUnitFixture(t)
					var original time.Time
					login := delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second)
					if known {
						original = s.now().Add(2 * time.Hour)
						login.credentials.value.refreshExpiresAt = &original
					}
					require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
					want := original
					wantToken := "old-refresh"
					body := `{"access_token":"access",` + tc.field
					if rotate {
						body += `,"refresh_token":"new-refresh"`
						wantToken = "new-refresh"
					}
					body += `}`
					s.refreshIdentity = func(ctx context.Context, _ *FederatedProvider, _, _, _ string) (*FederatedRefreshResult, error) {
						req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://idp.example.test/token", nil)
						require.NoError(t, err)
						tok, receivedAt, err := postFederatedRefresh(federatedHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
							return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
						}), req)
						require.NoError(t, err)
						// Match RefreshFederatedIdentity's production normalization.
						credentials := federatedRefreshCredentials(tok, receivedAt)
						if tc.seconds > 0 {
							want = receivedAt.Add(time.Duration(tc.seconds) * time.Second)
							require.Equal(t, &want, credentials.RefreshExpiresAt())
						} else {
							require.Nil(t, credentials.RefreshExpiresAt())
						}
						return &FederatedRefreshResult{Credentials: credentials}, nil
					}
					_, err := s.Resolve(t.Context(), b, allow)
					require.ErrorIs(t, err, ErrDelegationReauthentication)
					credential, err := store.load(t.Context(), b)
					require.NoError(t, err)
					require.Equal(t, want, credential.refreshExpiry)
					require.Equal(t, wantToken, delegationPlain(t, s, credential.refresh))
				})
			}
		}
	}
}
