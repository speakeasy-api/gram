package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestFederatedExplicitDelegationRetry(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "cancel", "csrf", "tenant", "unresolved", "authorizer_mismatch", "membership", "account_switch", "subject_switch", "impersonated", "missing_provenance", "missing_binding", "missing_subject", "provider_unavailable", "issuer_drift", "client_drift", "retry_used", "unsupported_action"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, f := newFederationLoginFixture(t, true, true)
			var handoffs []mcp.AuthorizedFederatedLogin
			var lookups []mcp.FederatedOfflineRequest
			f.ti.service.SetFederatedLoginConsumer(&offlineLoginConsumer{
				lookup: func(_ context.Context, r mcp.FederatedOfflineRequest) (bool, error) {
					lookups = append(lookups, r)
					return r.ExplicitRetry, nil
				},
				consume: func(_ context.Context, r mcp.AuthorizedFederatedLogin) error {
					handoffs = append(handoffs, r)
					return nil
				},
			})
			callback := func(q url.Values, cookie *http.Cookie) (*httptest.ResponseRecorder, error) {
				req := httptest.NewRequest(http.MethodGet, f.ti.serverURL.String()+"/mcp/idp_callback?"+q.Encode(), nil).WithContext(ctx)
				req.AddCookie(cookie)
				response := httptest.NewRecorder()
				err := f.ti.service.HandleIDPCallback(response, req)
				if err != nil {
					return response, fmt.Errorf("perform federation request: %w", err)
				}
				return response, nil
			}
			_, id, nonce, challenge, initial, cookie := f.begin(t, ctx, false)
			f.provider.issueCode(t, "initial", federationToken{nonce: nonce, challenge: challenge, email: mockidp.MockUserEmail, issuer: f.provider.URL, secret: "selected-secret", verified: true})
			response, err := callback(url.Values{"state": {id}, "code": {"initial"}, "iss": {f.provider.URL}}, cookie)
			require.NoError(t, err)
			consent, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			state, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consent.Query().Get("state"))
			require.NoError(t, err)
			require.Nil(t, state.Federation)
			require.NotNil(t, state.FederatedBinding)
			require.Equal(t, initial.Federation.IssuerID, state.FederatedBinding.IssuerID)
			require.Equal(t, initial.Federation.ClientID, state.FederatedBinding.ClientID)
			require.False(t, state.DelegationRetryUsed)
			require.Len(t, handoffs, 1)
			require.Len(t, lookups, 1)
			require.False(t, lookups[0].ExplicitRetry, "ordinary login respects refusal")
			state.CSRFToken = "validated-consent-csrf"
			endpoint, err := f.ti.service.LoadResolvedMcpEndpointBySlug(ctx, f.ti.logger, f.toolsetSlug, "mcp")
			require.NoError(t, err)
			form := url.Values{"action": {"retry_delegation"}, "state": {state.ID}, "csrf_token": {state.CSRFToken}, "human_id": {"posted-attacker"}, "client_id": {"posted-attacker"}}
			switch scenario {
			case "impersonated":
				impersonated := true
				state.AuthorizerImpersonated = &impersonated
			case "missing_provenance":
				state.AuthorizerImpersonated = nil
			case "missing_subject":
				state.FederatedBinding.Subject = ""
			case "provider_unavailable":
				// Fail the provider lookup without changing the trusted configuration.
				f.ti.conn.Close()
			case "missing_binding":
				state.FederatedBinding = nil
			case "issuer_drift":
				state.FederatedBinding.IssuerID = uuid.New()
			case "client_drift":
				state.FederatedBinding.ClientID = uuid.New()
			case "retry_used":
				state.DelegationRetryUsed = true
			case "unsupported_action":
				form.Set("action", "unsupported")
			case "csrf":
				form.Set("csrf_token", "wrong")
			case "tenant":
				endpoint.OrganizationID = f.otherOrg
			case "unresolved":
				state.Subject = nil
			case "authorizer_mismatch":
				state.AuthorizerUserID = "different-human"
			case "membership":
				f.resolver.hasAccessOK = false
			}
			require.NoError(t, f.ti.authnChallengeCache.Store(ctx, state))
			action := func() (*httptest.ResponseRecorder, error) {
				req := httptest.NewRequest(http.MethodPost, "/mcp/"+f.toolsetSlug+"/connect/remote-session", strings.NewReader(form.Encode())).WithContext(ctx)
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.AddCookie(cookie)
				response := httptest.NewRecorder()
				err := f.ti.service.ServeConsentAction(response, req, endpoint)
				if err != nil {
					return response, fmt.Errorf("perform federation request: %w", err)
				}
				return response, nil
			}
			actionResponse, err := action()
			switch scenario {
			case "csrf", "tenant", "unresolved", "authorizer_mismatch", "membership", "impersonated", "missing_provenance", "missing_binding", "missing_subject", "provider_unavailable", "issuer_drift", "client_drift", "retry_used", "unsupported_action":
				require.Error(t, err)
				if scenario == "provider_unavailable" {
					var failure *oops.ShareableError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, oops.CodeUnavailable, failure.Code)
				}
				if scenario == "unsupported_action" {
					require.ErrorContains(t, err, "retry_delegation")
				}
				require.Empty(t, actionResponse.Header().Get("Location"))
				require.Len(t, handoffs, 1)
				require.Len(t, lookups, 1)
				_, err = f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+state.ID)
				require.NoError(t, err, "invalid actions do not consume consent state")
				return
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusSeeOther, actionResponse.Code)
			_, err = action()
			require.Error(t, err, "retry POST consumes old consent state")
			bootstrap := httptest.NewRequest(http.MethodGet, actionResponse.Header().Get("Location"), nil).WithContext(ctx)
			retryCookies := actionResponse.Result().Cookies()
			require.Len(t, retryCookies, 1)
			for _, wrongCookie := range []bool{false, true} {
				transferred := httptest.NewRequest(http.MethodGet, bootstrap.URL.String(), nil).WithContext(ctx)
				if wrongCookie {
					cookie := *retryCookies[0]
					cookie.Value = "different-browser"
					transferred.AddCookie(&cookie)
				}
				response := httptest.NewRecorder()
				require.NoError(t, f.ti.service.HandleIDPCallback(response, transferred))
				assertFederationErrorRedirect(t, response)
				_, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+bootstrap.URL.Query().Get("state"))
				require.NoError(t, err, "missing or wrong browser proof must preserve retry state")
				require.Len(t, handoffs, 1)
				require.Len(t, lookups, 1)
			}
			bootstrap.AddCookie(retryCookies[0])
			begin := httptest.NewRecorder()
			require.NoError(t, f.ti.service.HandleIDPCallback(begin, bootstrap))
			target, err := url.Parse(begin.Header().Get("Location"))
			require.NoError(t, err)
			require.NotContains(t, target.Query().Get("scope"), "offline_access")
			require.Empty(t, target.Query().Get("prompt"))
			renewed, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+target.Query().Get("state"))
			require.NoError(t, err)
			require.Equal(t, initial.CreatedAt, renewed.CreatedAt, "explicit retry does not extend challenge lifetime")
			require.Equal(t, mockidp.MockUserID, renewed.Federation.ValidatedUserID)
			require.True(t, renewed.Federation.ExplicitRetry)
			require.Equal(t, f.provider.URL, renewed.Federation.ValidatedIdentity.Issuer)
			require.Equal(t, "upstream-human", renewed.Federation.ValidatedIdentity.Subject)
			require.True(t, renewed.DelegationRetryUsed)
			require.NotNil(t, renewed.AuthorizerImpersonated)
			require.False(t, *renewed.AuthorizerImpersonated)
			require.False(t, renewed.Federation.OfflineRequested)
			if scenario == "account_switch" {
				duplicate := "retry-other-" + uuid.NewString()
				q := usersrepo.New(f.ti.conn)
				require.NoError(t, q.DuplicateOrganizationUserEmailFixture(ctx, usersrepo.DuplicateOrganizationUserEmailFixtureParams{DuplicateUserID: duplicate, UserID: mockidp.MockUserID, OrganizationID: f.organizationID}))
				_, err := orgsrepo.New(f.ti.conn).UpsertOrganizationUserRelationship(ctx, orgsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: f.organizationID, UserID: conv.ToPGText(duplicate)})
				require.NoError(t, err)
				require.NoError(t, q.SetOrganizationUserEmailFixture(ctx, usersrepo.SetOrganizationUserEmailFixtureParams{Email: "original-human-renamed@example.test", UserID: mockidp.MockUserID, OrganizationID: f.organizationID}))
			}
			subject := "upstream-human"
			if scenario == "subject_switch" {
				subject = "another-upstream-human"
			}
			f.provider.issueCode(t, "renewed", federationToken{subject: subject, nonce: target.Query().Get("nonce"), challenge: target.Query().Get("code_challenge"), email: mockidp.MockUserEmail, issuer: f.provider.URL, secret: "selected-secret", verified: true})
			renewedResponse, err := callback(url.Values{"state": {renewed.ID}, "code": {"renewed"}, "iss": {f.provider.URL}}, retryCookies[0])
			if scenario == "account_switch" || scenario == "subject_switch" {
				require.Len(t, handoffs, 1, "different provisioned human must be rejected before retention")
				require.Len(t, lookups, 1, "different human cannot reset refusal")
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, renewedResponse.Code)
				require.Contains(t, renewedResponse.Header().Get("Location"), "error=")
				return
			}
			require.NoError(t, err)
			require.Len(t, handoffs, 2)
			require.Len(t, lookups, 2)
			require.True(t, lookups[1].ExplicitRetry)
			optional, err := url.Parse(renewedResponse.Header().Get("Location"))
			require.NoError(t, err)
			require.Contains(t, optional.Query().Get("scope"), "offline_access")
			require.Equal(t, "consent", optional.Query().Get("prompt"))
			// Optional consent preserves the existing browser proof.
			optionalCookie := retryCookies[0]
			q := url.Values{"state": {optional.Query().Get("state")}, "iss": {f.provider.URL}}
			if scenario == "cancel" {
				q.Set("error", "access_denied")
			} else {
				f.provider.issueCode(t, "optional", federationToken{nonce: optional.Query().Get("nonce"), challenge: optional.Query().Get("code_challenge"), email: mockidp.MockUserEmail, issuer: f.provider.URL, secret: "selected-secret", verified: true})
				q.Set("code", "optional")
			}
			done, err := callback(q, optionalCookie)
			require.NoError(t, err)
			require.Len(t, handoffs, 3)
			require.Equal(t, scenario == "cancel", handoffs[2].OptionalRefused)
			require.Len(t, lookups, 2, "one optional round trip per retry action")
			require.NotContains(t, done.Header().Get("Location"), f.provider.URL)
			completedURL, err := url.Parse(done.Header().Get("Location"))
			require.NoError(t, err)
			completed, err := f.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+completedURL.Query().Get("state"))
			require.NoError(t, err)
			require.Nil(t, completed.Federation)
			require.True(t, completed.DelegationRetryUsed)
			require.Equal(t, state.FederatedBinding, completed.FederatedBinding)
			cookie = retryCookies[0]
			form.Set("state", completed.ID)
			form.Set("csrf_token", completed.CSRFToken)
			_, err = action()
			require.ErrorContains(t, err, "already used")
			require.Len(t, lookups, 2, "completed retry cannot prompt again")
			_, err = callback(q, optionalCookie)
			require.Error(t, err)
		})
	}
}
