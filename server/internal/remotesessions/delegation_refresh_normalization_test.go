package remotesessions

import (
	"context"
	"crypto/x509"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
				expired bool
			}{
				{name: "alias", field: `"refresh_expires_in":120`, seconds: 120},
				{name: "standard takes precedence", field: `"refresh_expires_in":120,"refresh_token_timeout":60`, seconds: 60},
				{name: "authorization bound", field: `"refresh_expires_in":120,"authorization_expires_in":30`, seconds: 30},
				{name: "overflow ignored", field: `"refresh_expires_in":9223372036854775807`},
				{name: "negative ignored", field: `"refresh_expires_in":-1`},
				{name: "zero alias ignored", field: `"refresh_expires_in":0`},
				{name: "retained bound expires during request", field: `"refresh_expires_in":0`, expired: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					s, store, p, b, allow := newDelegationUnitFixture(t)
					now := time.Now()
					s.now = func() time.Time { return now }
					var body string
					upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						assert.Equal(t, http.MethodPost, r.Method)
						_, _ = io.WriteString(w, body)
					}))
					defer upstream.Close()
					roots := x509.NewCertPool()
					roots.AddCert(upstream.Certificate())
					policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(roots))
					require.NoError(t, err)
					p.issuer.Issuer = upstream.URL
					p.metadata.Issuer = upstream.URL
					p.metadata.AuthorizationEndpoint = upstream.URL + "/authorize"
					p.metadata.TokenEndpoint = upstream.URL + "/token"
					p.metadata.JwksURI = upstream.URL + "/jwks"
					p.client.ClientSecretEncrypted.String, err = s.enc.Encrypt([]byte("secret"))
					require.NoError(t, err)
					manager := &ChallengeManager{enc: s.enc, policy: policy}
					var original time.Time
					login := delegationLogin(p, s.now(), "old-id", "old-refresh", 30*time.Second)
					if known {
						original = s.now().Add(2 * time.Hour)
						login.credentials.value.refreshExpiresAt = &original
					}
					require.NoError(t, s.RetainVerifiedLogin(t.Context(), p, b.HumanID, login, true))
					want := original
					wantToken := "old-refresh"
					body = `{"access_token":"access",` + tc.field
					if rotate {
						body += `,"refresh_token":"new-refresh"`
						wantToken = "new-refresh"
					}
					body += `}`
					s.refreshIdentity = func(ctx context.Context, p *FederatedProvider, token, subject, nonce string) (*FederatedRefreshResult, error) {
						result, err := manager.RefreshFederatedIdentity(ctx, p, token, subject, nonce)
						require.NoError(t, err)
						now = result.Credentials.receivedAt
						if tc.seconds > 0 {
							want = now.Add(time.Duration(tc.seconds) * time.Second)
							require.Equal(t, &want, result.Credentials.RefreshExpiresAt())
						} else {
							require.Nil(t, result.Credentials.RefreshExpiresAt())
							if known {
								require.True(t, original.After(s.now()), "retained bound must still be live at receipt")
							}
						}
						if known && tc.expired {
							now = original.Add(time.Second)
							want = time.Time{}
							wantToken = ""
						}
						return result, nil
					}
					_, err = s.Resolve(t.Context(), b, allow)
					require.ErrorIs(t, err, ErrDelegationReauthentication)
					credential, err := store.load(t.Context(), b)
					require.NoError(t, err)
					if want.IsZero() {
						require.True(t, credential.RefreshExpiresAt.Time.IsZero())
					} else {
						require.WithinDuration(t, want, credential.RefreshExpiresAt.Time, time.Microsecond)
					}
					if wantToken == "" {
						require.Empty(t, credential.RefreshTokenEncrypted.String)
					} else {
						require.Equal(t, wantToken, delegationPlain(t, s, credential.RefreshTokenEncrypted.String))
					}
				})
			}
		}
	}
}
