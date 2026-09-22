package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/stretchr/testify/require"
)

type offlineLoginConsumer struct {
	lookup  func(context.Context, mcp.FederatedOfflineRequest) (bool, error)
	consume func(context.Context, mcp.AuthorizedFederatedLogin) error
}

func (c *offlineLoginConsumer) ShouldRequestFederatedOffline(ctx context.Context, r mcp.FederatedOfflineRequest) (bool, error) {
	return c.lookup(ctx, r)
}
func (c *offlineLoginConsumer) ConsumeFederatedLogin(ctx context.Context, r mcp.AuthorizedFederatedLogin) error {
	return c.consume(ctx, r)
}

func TestFederatedOptionalOfflineConsent(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"no_refresh", "cancel", "provider_error", "account_switch", "suppressed", "membership_denied"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, f := newFederationLoginFixture(t, scenario != "membership_denied", true)
			var logins []mcp.AuthorizedFederatedLogin
			lookups := 0
			f.ti.service.SetFederatedLoginConsumer(&offlineLoginConsumer{
				lookup: func(_ context.Context, request mcp.FederatedOfflineRequest) (bool, error) {
					lookups++
					require.Equal(t, mockidp.MockUserID, request.UserID)
					require.Equal(t, f.organizationID, request.OrganizationID)
					require.Equal(t, f.clientID, request.TrustedClientID)
					require.Equal(t, []string{mockidp.MockUserID}, f.resolver.memberChecks, "membership must precede per-human policy lookup")
					require.NotEmpty(t, request.ConfigurationHash)
					require.Equal(t, request.Provider.OfflineConfigurationHash(), request.ConfigurationHash)
					return scenario != "suppressed", nil
				},
				consume: func(_ context.Context, login mcp.AuthorizedFederatedLogin) error {
					logins = append(logins, login)
					if login.Identity != nil {
						return login.Identity.WithCredentials(func(c remotesessions.EphemeralFederatedCredentials) error {
							require.NotEmpty(t, c.IDToken())
							require.Empty(t, c.RefreshToken())
							return nil
						})
					}
					require.True(t, login.OptionalRefused)
					return nil
				},
			})
			_, id, nonce, challenge, initial, cookie := f.begin(t, ctx, false)
			require.Zero(t, lookups, "unknown identity must not look up another human's refusal")
			require.False(t, initial.Federation.OfflineRequested)
			f.provider.issueCode(t, "minimal", federationToken{nonce: nonce, email: mockidp.MockUserEmail, issuer: f.provider.URL, secret: "selected-secret", challenge: challenge, verified: true})
			callback := func(q url.Values, cookie *http.Cookie) (*httptest.ResponseRecorder, error) {
				req := httptest.NewRequest(http.MethodGet, f.ti.serverURL.String()+"/mcp/idp_callback?"+q.Encode(), nil).WithContext(ctx)
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				err := f.ti.service.HandleIDPCallback(rec, req)
				if err != nil {
					return rec, fmt.Errorf("perform federation request: %w", err)
				}
				return rec, nil
			}
			first, err := callback(url.Values{"state": {id}, "code": {"minimal"}, "iss": {f.provider.URL}}, cookie)
			if scenario == "membership_denied" {
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, first.Code)
				require.Contains(t, first.Header().Get("Location"), "error=")
				require.Empty(t, logins)
				require.Zero(t, lookups)
				return
			}
			require.NoError(t, err)
			require.Len(t, logins, 1)
			require.False(t, logins[0].OfflineRequested)
			require.False(t, logins[0].OptionalRefused)
			if scenario == "suppressed" {
				require.NotContains(t, first.Header().Get("Location"), f.provider.URL)
				require.Equal(t, 1, lookups)
				return
			}
			offline, err := url.Parse(first.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, f.provider.URL+"/authorize", offline.Scheme+"://"+offline.Host+offline.Path)
			require.Contains(t, offline.Query().Get("scope"), "offline_access")
			require.Contains(t, offline.Query().Get("scope"), "profile")
			require.Equal(t, "consent", offline.Query().Get("prompt"))
			require.NotEqual(t, id, offline.Query().Get("state"))
			require.NotEqual(t, nonce, offline.Query().Get("nonce"))
			state, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+offline.Query().Get("state"))
			require.NoError(t, err)
			require.Equal(t, initial.FlowID, state.FlowID)
			require.Equal(t, mockidp.MockUserID, state.Federation.ValidatedUserID)
			require.True(t, state.Federation.OfflineRequested)
			require.Error(t, state.Federation.ValidatedIdentity.WithCredentials(func(remotesessions.EphemeralFederatedCredentials) error {
				t.Fatal("credentials cached in state")
				return nil
			}))
			// Optional consent preserves the existing browser proof.
			secondCookie := cookie
			q := url.Values{"state": {state.ID}, "iss": {f.provider.URL}}
			switch scenario {
			case "cancel":
				q.Set("error", "access_denied")
			case "provider_error":
				q.Set("error", "server_error")
			default:
				subject := "upstream-human"
				if scenario == "account_switch" {
					subject = "another-upstream-human"
				}
				f.provider.issueCode(t, "offline", federationToken{nonce: offline.Query().Get("nonce"), email: mockidp.MockUserEmail, issuer: f.provider.URL, secret: "selected-secret", challenge: offline.Query().Get("code_challenge"), verified: true, subject: subject})
				q.Set("code", "offline")
			}
			second, err := callback(q, secondCookie)
			if scenario == "provider_error" || scenario == "account_switch" {
				// Third-party failures can be encoded in the client redirect instead of
				// returning a handler error. Neither may reach the credential consumer.
				require.Len(t, logins, 1)
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, second.Code)
				require.Contains(t, second.Header().Get("Location"), "error=")
			} else {
				require.NoError(t, err)
				require.Len(t, logins, 2)
				require.True(t, logins[1].OfflineRequested)
				require.Equal(t, scenario == "cancel", logins[1].OptionalRefused)
				require.NotContains(t, second.Header().Get("Location"), f.provider.URL, "at most one offline round trip")
			}
			require.Equal(t, 1, lookups)
			_, err = callback(q, secondCookie)
			require.Error(t, err, "optional callback is single use")
		})
	}
}
