package mcp_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

type federatedLoginConsumerFunc func(context.Context, mcp.AuthorizedFederatedLogin) error

func (f federatedLoginConsumerFunc) ConsumeFederatedLogin(ctx context.Context, login mcp.AuthorizedFederatedLogin) error {
	return f(ctx, login)
}

// Exercise the browser protocol with real PostgreSQL/Redis and signed TLS OIDC
// responses. Only the external provider and final membership check are mocked.
func TestFederatedLogin(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"success", "first_party", "missing_cookie", "wrong_cookie", "expired_state", "changed_client", "deleted_client", "changed_issuer", "missing_response_issuer", "wrong_response_issuer", "wrong_nonce", "missing_email", "unprovisioned", "deleted_directory_user", "membership_denied", "consumer", "consumer_failure", "unverified_email", "wrong_tenant_directory", "other_provider_token", "rotated_credentials", "deleted_gram_user", "nonexistent_gram_account", "foreign_membership", "ambiguous_gram_accounts", "workos_deleted_directory"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "federation-test"))
			require.NoError(t, err)
			var nonce, issuerURL string
			var expectedSecret atomic.Value
			expectedSecret.Store("selected-secret")
			var otherProviderCalls atomic.Int32
			otherProvider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				otherProviderCalls.Add(1)
				http.Error(w, "wrong tenant provider selected", http.StatusForbidden)
			}))
			t.Cleanup(otherProvider.Close)
			var exchanges atomic.Int32
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuerURL, "authorization_endpoint": issuerURL + "/authorize", "token_endpoint": issuerURL + "/token", "jwks_uri": issuerURL + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"ES256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "code_challenge_methods_supported": []string{"S256"}, "authorization_response_iss_parameter_supported": true})
				case "/jwks":
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: "federation-test", Algorithm: "ES256", Use: "sig"}}})
				case "/token":
					exchanges.Add(1)
					id, secret, ok := r.BasicAuth()
					if !ok || id != "selected-client" || secret != expectedSecret.Load().(string) {
						http.Error(w, "incorrect client", http.StatusUnauthorized)
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Errorf("parse token request: %v", err)
						return
					}
					if r.Form.Get("code") != "one-use-code" || r.Form.Get("code_verifier") == "" {
						t.Error("missing code or upstream PKCE")
					}
					email := mockidp.MockUserEmail
					if scenario == "missing_email" {
						email = ""
					}
					if scenario == "unprovisioned" {
						email = "unprovisioned@example.test"
					}
					tokenNonce := nonce
					if scenario == "wrong_nonce" {
						tokenNonce = "wrong-nonce"
					}
					tokenIssuer := issuerURL
					if scenario == "other_provider_token" {
						tokenIssuer = otherProvider.URL
					}
					raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: tokenIssuer, Subject: "upstream-human", Audience: jwt.Audience{"selected-client"}, IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute))}).Claims(map[string]any{"nonce": tokenNonce, "email": email, "email_verified": scenario != "unverified_email"}).Serialize()
					if err != nil {
						t.Errorf("sign token: %v", err)
						return
					}
					// Deliberately no refresh_token: login is not a retention workflow.
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ephemeral-access-token", "token_type": "Bearer", "expires_in": 60, "id_token": raw})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(provider.Close)
			issuerURL = provider.URL
			roots := x509.NewCertPool()
			roots.AddCert(provider.Certificate())
			roots.AddCert(otherProvider.Certificate())
			resolver := &mockIdentityResolver{hasAccessOK: scenario != "membership_denied"}
			ctx, ti := newTestMCPServiceWithTunnelPublicConfig(t, resolver, mcp.TunnelPublicConfig{}, guardian.WithTLSRootCAs(roots))
			// Federation requires an HTTPS callback, unlike the shared HTTP fixture.
			ti.serverURL.Scheme = "https"
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			otherOrg := "org_" + uuid.NewString()
			require.NoError(t, orgsrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgsrepo.CreateOrganizationMetadataParams{ID: otherOrg, Name: "Other federation organization", Slug: "other-fed-" + uuid.NewString()}))
			// Same external client id and user email in another tenant must not affect
			// provider selection or provisioned-human resolution in this organization.
			otherIssuer := createTrustedIDJAGIssuer(t, ctx, ti, otherOrg, otherProvider.URL, otherProvider.URL+"/jwks")
			_, err = remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(otherOrg), RemoteSessionIssuerID: otherIssuer.TrustedRemoteSessionIssuerID.UUID, ClientID: "selected-client", TokenEndpointAuthMethod: conv.ToPGText("none"), Scope: []string{"openid", "email"}})
			require.NoError(t, err)
			seedIDJAGDirectoryUser(t, ctx, ti, otherOrg)
			remote, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
				OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "federation-" + uuid.NewString(), Issuer: provider.URL,
				AuthorizationEndpoint: conv.ToPGText(provider.URL + "/authorize"), TokenEndpoint: conv.ToPGText(provider.URL + "/token"), JwksUri: conv.ToPGText(provider.URL + "/jwks"),
				ScopesSupported: []string{"openid", "email"}, GrantTypesSupported: []string{"authorization_code"}, ResponseTypesSupported: []string{"code"}, TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"}, CodeChallengeMethodsSupported: []string{"S256"}, IDTokenSigningAlgValuesSupported: []string{"ES256"}, AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: true, Valid: true},
			})
			require.NoError(t, err)
			secret, err := ti.enc.Encrypt([]byte("selected-secret"))
			require.NoError(t, err)
			// Insert an unselected client first: selection must follow the explicit FK.
			_, err = remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), RemoteSessionIssuerID: remote.ID, ClientID: "not-selected", ClientSecretEncrypted: conv.ToPGText(secret), TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"), Scope: []string{"openid"}})
			require.NoError(t, err)
			client, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), RemoteSessionIssuerID: remote.ID, ClientID: "selected-client", ClientSecretEncrypted: conv.ToPGText(secret), TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"), Scope: []string{"openid", "email"}})
			require.NoError(t, err)
			issuer, err := usersessionsrepo.New(ti.conn).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "federation-" + uuid.NewString(), AuthnChallengeMode: "chain", SessionDuration: pgtype.Interval{Microseconds: int64(8 * time.Hour / time.Microsecond), Valid: true}, TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true}})
			require.NoError(t, err)
			toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			toolset, err = toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true}, Slug: toolset.Slug, ProjectID: *ac.ProjectID})
			require.NoError(t, err)
			var consumed int
			var retainedIdentity *remotesessions.FederatedIdentity
			if scenario != "success" && scenario != "first_party" && scenario != "rotated_credentials" {
				ti.service.SetFederatedLoginConsumer(federatedLoginConsumerFunc(func(_ context.Context, login mcp.AuthorizedFederatedLogin) error {
					consumed++
					require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks, "membership must precede credential handoff")
					require.Equal(t, ac.ActiveOrganizationID, login.OrganizationID)
					require.Equal(t, mockidp.MockUserID, login.UserID)
					require.Equal(t, issuer.ID, login.UserSessionIssuerID)
					require.Equal(t, remote.ID, login.TrustedIssuerID)
					require.Equal(t, client.ID, login.TrustedClientID)
					retainedIdentity = login.Identity
					require.NoError(t, login.Identity.WithCredentials(func(credentials remotesessions.EphemeralFederatedCredentials) error {
						require.NotEmpty(t, credentials.IDToken())
						require.Empty(t, credentials.RefreshToken())
						return nil
					}))
					require.Error(t, login.Identity.WithCredentials(func(remotesessions.EphemeralFederatedCredentials) error {
						t.Error("credentials consumed twice")
						return nil
					}))
					if scenario == "consumer_failure" {
						return errors.New("sensitive upstream failure")
					}
					return nil
				}))
			}
			downstream := createIDJAGClient(t, ctx, ti, issuer.ID)
			seedIDJAGDirectoryUser(t, ctx, ti, ac.ActiveOrganizationID)
			// AIM-75: NULL directory link resolves only an existing same-org human.
			_, err = ti.conn.Exec(ctx, `UPDATE directory_users SET user_id = NULL WHERE organization_id = $1`, ac.ActiveOrganizationID)
			require.NoError(t, err)
			// Mutate only this tenant's fixture before recording the no-upsert
			// snapshot. The directory remains NULL-linked in every fallback case.
			switch scenario {
			case "deleted_gram_user":
				_, err = ti.conn.Exec(ctx, `UPDATE users SET deleted_at = now() WHERE id = $1 AND EXISTS (SELECT 1 FROM organization_user_relationships WHERE user_id = users.id AND organization_id = $2)`, mockidp.MockUserID, ac.ActiveOrganizationID)
			case "nonexistent_gram_account":
				_, err = ti.conn.Exec(ctx, `UPDATE users SET email = 'different-account@example.test' WHERE id = $1 AND EXISTS (SELECT 1 FROM organization_user_relationships WHERE user_id = users.id AND organization_id = $2)`, mockidp.MockUserID, ac.ActiveOrganizationID)
			case "foreign_membership":
				_, err = ti.conn.Exec(ctx, `INSERT INTO organization_user_relationships (organization_id, user_id) SELECT $1, user_id FROM organization_user_relationships WHERE organization_id = $2 AND user_id = $3`, otherOrg, ac.ActiveOrganizationID, mockidp.MockUserID)
				require.NoError(t, err)
				_, err = ti.conn.Exec(ctx, `DELETE FROM organization_user_relationships WHERE organization_id = $1 AND user_id = $2`, ac.ActiveOrganizationID, mockidp.MockUserID)
			case "ambiguous_gram_accounts":
				// The raw email is unique, but resolution deliberately matches case
				// insensitively. Both active humans have same-organization membership.
				duplicateID := "ambiguous-" + uuid.NewString()
				_, err = ti.conn.Exec(ctx, `INSERT INTO users (id, email, display_name) SELECT $1, upper(u.email), 'Ambiguous test human' FROM users u JOIN organization_user_relationships m ON m.user_id = u.id WHERE u.id = $2 AND m.organization_id = $3`, duplicateID, mockidp.MockUserID, ac.ActiveOrganizationID)
				require.NoError(t, err)
				_, err = ti.conn.Exec(ctx, `INSERT INTO organization_user_relationships (organization_id, user_id) VALUES ($1, $2)`, ac.ActiveOrganizationID, duplicateID)
			case "workos_deleted_directory":
				_, err = ti.conn.Exec(ctx, `UPDATE directory_users SET workos_deleted_at = now() WHERE organization_id = $1`, ac.ActiveOrganizationID)
			}
			require.NoError(t, err)
			var beforeUsers string
			require.NoError(t, ti.conn.QueryRow(ctx, `SELECT coalesce(string_agg(id || ':' || xmin::text, ',' ORDER BY id), '') FROM users`).Scan(&beforeUsers))
			query := url.Values{"response_type": {"code"}, "client_id": {downstream.ClientID}, "redirect_uri": {"http://127.0.0.1/callback"}, "state": {"downstream-state"}, "code_challenge": {"downstream-pkce"}, "code_challenge_method": {"S256"}}
			route := chi.NewRouteContext()
			route.URLParams.Add("mcpSlug", toolset.McpSlug.String)
			req := httptest.NewRequest(http.MethodGet, "/mcp/"+toolset.McpSlug.String+"/authorize?"+query.Encode(), nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
			start := httptest.NewRecorder()
			if scenario == "first_party" {
				err = ti.service.HandleFirstPartyConnect(start, req)
			} else {
				err = ti.service.HandleAuthorize(start, req)
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusFound, start.Code)
			bootstrap, err := url.Parse(start.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, ti.serverURL.Host, bootstrap.Host)
			require.Equal(t, "/mcp/idp_callback", bootstrap.Path)
			require.Equal(t, "1", bootstrap.Query().Get("federated_start"))
			initialID := bootstrap.Query().Get("state")
			require.NotEmpty(t, initialID)
			initial, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+initialID)
			require.NoError(t, err)
			require.NotNil(t, initial.Federation)
			require.Empty(t, initial.Federation.BrowserHash)
			begin := httptest.NewRecorder()
			require.NoError(t, ti.service.HandleIDPCallback(begin, httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx)))
			upstream, err := url.Parse(begin.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, provider.URL+"/authorize", upstream.Scheme+"://"+upstream.Host+upstream.Path)
			require.Equal(t, "selected-client", upstream.Query().Get("client_id"))
			require.Equal(t, ti.serverURL.String()+"/mcp/idp_callback", upstream.Query().Get("redirect_uri"))
			id := upstream.Query().Get("state")
			require.NotEmpty(t, id)
			require.NotEqual(t, initialID, id)
			nonce = upstream.Query().Get("nonce")
			require.NotEmpty(t, nonce)
			state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+id)
			require.NoError(t, err)
			digest := sha256.Sum256([]byte(state.Federation.Verifier))
			require.Equal(t, base64.RawURLEncoding.EncodeToString(digest[:]), upstream.Query().Get("code_challenge"))
			require.Equal(t, "S256", upstream.Query().Get("code_challenge_method"))
			cookies := begin.Result().Cookies()
			require.Len(t, cookies, 1)
			cookie := cookies[0]
			require.Empty(t, cookie.Domain)
			require.Equal(t, "/", cookie.Path)
			require.True(t, cookie.Secure)
			require.True(t, cookie.HttpOnly)
			require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
			require.Contains(t, cookie.Name, "__Host-")
			require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, bootstrap.String(), nil).WithContext(ctx)), "bootstrap is single-use")
			switch scenario {
			case "expired_state":
				state.CreatedAt = time.Now().Add(-state.TTL() - time.Minute)
				require.NoError(t, ti.authnChallengeCache.Store(ctx, state))
			case "changed_client":
				_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET client_id = 'changed' WHERE id = $1 AND organization_id = $2`, client.ID, ac.ActiveOrganizationID)
			case "deleted_client":
				_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET deleted_at = now() WHERE id = $1 AND organization_id = $2`, client.ID, ac.ActiveOrganizationID)
			case "changed_issuer":
				_, err = ti.conn.Exec(ctx, `UPDATE user_session_issuers SET updated_at = updated_at + interval '1 second' WHERE id = $1 AND organization_id = $2`, issuer.ID, ac.ActiveOrganizationID)
			case "wrong_tenant_directory":
				_, err = ti.conn.Exec(ctx, `DELETE FROM directory_users WHERE organization_id = $1`, ac.ActiveOrganizationID)
			case "rotated_credentials":
				rotated, encryptErr := ti.enc.Encrypt([]byte("rotated-secret"))
				require.NoError(t, encryptErr)
				_, err = ti.conn.Exec(ctx, `UPDATE remote_session_clients SET client_secret_encrypted = $1 WHERE id = $2 AND organization_id = $3`, rotated, client.ID, ac.ActiveOrganizationID)
				expectedSecret.Store("rotated-secret")
			case "deleted_directory_user":
				_, err = ti.conn.Exec(ctx, `UPDATE directory_users SET deleted_at = now() WHERE organization_id = $1`, ac.ActiveOrganizationID)
			}
			require.NoError(t, err)
			callbackQuery := url.Values{"state": {id}, "code": {"one-use-code"}, "iss": {provider.URL}}
			if scenario == "missing_response_issuer" {
				callbackQuery.Del("iss")
			}
			if scenario == "wrong_response_issuer" {
				callbackQuery.Set("iss", "https://other.example.test")
			}
			callback := httptest.NewRequest(http.MethodGet, ti.serverURL.String()+"/mcp/idp_callback?"+callbackQuery.Encode(), nil).WithContext(ctx)
			if scenario == "wrong_cookie" {
				cookie.Value = "different-browser"
			}
			if scenario != "missing_cookie" {
				callback.AddCookie(cookie)
			}
			if scenario == "rotated_credentials" {
				require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), callback), "old flow must reject secret rotation")
				require.Zero(t, exchanges.Load())
				freshStart := httptest.NewRecorder()
				require.NoError(t, ti.service.HandleAuthorize(freshStart, req))
				freshBootstrap := httptest.NewRecorder()
				require.NoError(t, ti.service.HandleIDPCallback(freshBootstrap, httptest.NewRequest(http.MethodGet, freshStart.Header().Get("Location"), nil).WithContext(ctx)))
				freshURL, parseErr := url.Parse(freshBootstrap.Header().Get("Location"))
				require.NoError(t, parseErr)
				id = freshURL.Query().Get("state")
				nonce = freshURL.Query().Get("nonce")
				callbackQuery.Set("state", id)
				callback = httptest.NewRequest(http.MethodGet, ti.serverURL.String()+"/mcp/idp_callback?"+callbackQuery.Encode(), nil).WithContext(ctx)
				callback.AddCookie(freshBootstrap.Result().Cookies()[0])
			}
			result := httptest.NewRecorder()
			err = ti.service.HandleIDPCallback(result, callback)
			success := scenario == "success" || scenario == "first_party" || scenario == "consumer" || scenario == "rotated_credentials"
			if success {
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, result.Code)
				consent, err := url.Parse(result.Header().Get("Location"))
				require.NoError(t, err)
				require.Contains(t, consent.Path, "/connect")
				consentID := consent.Query().Get("state")
				require.NotEmpty(t, consentID)
				require.NotEqual(t, id, consentID)
				resolved, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+consentID)
				require.NoError(t, err)
				require.Nil(t, resolved.Federation)
				require.NotNil(t, resolved.Subject)
				require.Equal(t, mockidp.MockUserID, resolved.AuthorizerUserID)
				require.Equal(t, scenario == "first_party", resolved.FirstParty)
				require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks)
				require.EqualValues(t, 1, exchanges.Load())
				serialized, err := json.Marshal(resolved)
				require.NoError(t, err)
				require.NotContains(t, string(serialized), "ephemeral-access-token")
				require.NotContains(t, string(serialized), nonce)
			} else {
				require.Error(t, err)
				require.Empty(t, result.Header().Get("Location"))
				require.NotContains(t, err.Error(), mockidp.MockUserEmail)
				require.NotContains(t, err.Error(), "ephemeral-access-token")
				require.NotContains(t, err.Error(), "sensitive upstream failure")
				require.NotContains(t, err.Error(), expectedSecret.Load().(string))
			}
			switch scenario {
			case "deleted_gram_user", "nonexistent_gram_account", "foreign_membership", "ambiguous_gram_accounts", "workos_deleted_directory":
				require.EqualError(t, err, "Your account is not provisioned for this organization. Contact your administrator")
				require.Empty(t, resolver.memberChecks, "provisioned-human resolution must reject before the membership hook")
				require.Zero(t, consumed, "unresolved humans must never reach credential handoff")
			}
			require.Zero(t, otherProviderCalls.Load(), "never contact a provider from another organization")
			if scenario == "consumer" || scenario == "consumer_failure" {
				require.Equal(t, 1, consumed)
				require.Error(t, retainedIdentity.WithCredentials(func(remotesessions.EphemeralFederatedCredentials) error {
					t.Error("handoff replayed after callback")
					return nil
				}))
			} else {
				require.Zero(t, consumed)
			}
			if scenario == "membership_denied" {
				require.Equal(t, []string{mockidp.MockUserID}, resolver.memberChecks)
			}
			require.NotContains(t, resolver.calls, "CompleteIDPLogin", "federation must never upsert a user")
			var afterUsers string
			require.NoError(t, ti.conn.QueryRow(ctx, `SELECT coalesce(string_agg(id || ':' || xmin::text, ',' ORDER BY id), '') FROM users`).Scan(&afterUsers))
			require.Equal(t, beforeUsers, afterUsers)
			count := exchanges.Load()
			require.Error(t, ti.service.HandleIDPCallback(httptest.NewRecorder(), callback), "callback is single-use even on rejection")
			require.Equal(t, count, exchanges.Load(), "replay must not reach the token endpoint")
			if scenario == "missing_cookie" || scenario == "wrong_cookie" || scenario == "expired_state" || scenario == "changed_client" || scenario == "deleted_client" || scenario == "changed_issuer" || scenario == "missing_response_issuer" || scenario == "wrong_response_issuer" {
				require.Zero(t, exchanges.Load())
			} else {
				require.EqualValues(t, 1, exchanges.Load(), "identity and membership failures must exercise verified exchange")
			}
			require.LessOrEqual(t, consumed, 1, "callback replay must not repeat consumer")
		})
	}
}
