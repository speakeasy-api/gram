package remotesessions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

type jwtGrant struct {
	subject       string
	scope         string
	scopeInToken  bool
	responseScope *string
	opaque        bool
}

func jwtGrantHandler(t *testing.T, issuer *idTokenIssuer, clientID string, initial, refreshed jwtGrant) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		grant := initial
		if r.FormValue("grant_type") == "refresh_token" {
			grant = refreshed
		}
		accessToken := "opaque-access-token"
		if !grant.opaque {
			claims := accessTokenClaims(issuer.issuerURL, clientID)
			claims["sub"] = grant.subject
			claims["client_id"] = clientID
			claims["groups"] = []string{"admins"}
			if grant.scopeInToken {
				claims["scope"] = grant.scope
			}
			accessToken = mintAccessToken(t, issuer, new("at+jwt"), claims)
		}
		body := map[string]any{
			"access_token": accessToken, "refresh_token": "refresh-token",
			"token_type": "Bearer", "expires_in": 3600,
		}
		if grant.responseScope != nil {
			body["scope"] = *grant.responseScope
		}
		raw, err := json.Marshal(body)
		if !assert.NoError(t, err) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

func TestTokenResponseScopeTypeRejectedAtExchangeAndRefresh(t *testing.T) {
	t.Parallel()

	for _, malformed := range []string{`["read"]`} {
		t.Run("exchange/"+malformed, func(t *testing.T) {
			t.Parallel()
			_, env, _, err := driveSyntheticLogin(t, "scope-type-exchange-"+strings.Trim(malformed, `[]"`), func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","scope":` + malformed + `}`))
			})
			require.ErrorContains(t, err, "upstream token exchange failed")
			_, loadErr := env.q.GetActiveRemoteSession(t.Context(), repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.Error(t, loadErr)
		})

		t.Run("refresh/"+malformed, func(t *testing.T) {
			t.Parallel()
			ctx, env := newSyntheticExpiryEnv(t, "scope-type-refresh-"+strings.Trim(malformed, `[]"`), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.FormValue("grant_type") == "refresh_token" {
					_, _ = w.Write([]byte(`{"access_token":"new-access","token_type":"Bearer","scope":` + malformed + `}`))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","scope":""}`))
			})
			_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
			require.ErrorContains(t, err, "token response scope must be a string")
			stored, loadErr := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, loadErr)
			require.Equal(t, env.session.UpdatedAt, stored.UpdatedAt)
		})
	}
}

func TestJWTAccessTokenExchangeOrchestration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		responseScope *string
		wantScopes    []string
	}{
		{name: "JWT scope when response omits scope", wantScopes: []string{"jwt:read"}},
		{name: "present empty response scope wins", responseScope: new(""), wantScopes: []string{}},
		{name: "present response scope wins", responseScope: new("response:read"), wantScopes: []string{"response:read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issuer := newIDTokenIssuer(t)
			slug := "jwt-exchange-" + strings.NewReplacer(" ", "-", ":", "-").Replace(tc.name)
			clientID := "synthetic-cid-" + slug
			grant := jwtGrant{subject: "user-123", scope: "jwt:read", scopeInToken: true, responseScope: tc.responseScope}
			_, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, grant, grant),
				withIDTokenIssuer(issuer), withIssuerScopes("requested:read"), withClientScope("requested:read"))

			require.Equal(t, remotesessions.IdentitySourceJWTAccessToken, env.session.IdentitySource.String)
			require.Equal(t, "user-123", env.session.UpstreamSubject.String)
			require.Equal(t, "owner@example.com", env.session.UpstreamEmail.String)
			require.Equal(t, tc.wantScopes, env.session.Scopes)
			require.Contains(t, string(env.session.Enrichment), "jwt_access_token")
			require.NotContains(t, string(env.session.Enrichment), "groups", "only allowlisted claims are retained")
			require.False(t, env.session.LastValidatedAt.Valid, "JWT enrichment is not live validation")
			require.False(t, env.session.ValidationStatus.Valid)
		})
	}
}

func TestJWTAccessTokenExchangeAcceptsClientAudienceWhenResourceIndicatorUnsupported(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const slug = "jwt-exchange-resource-unsupported"
	const clientID = "synthetic-cid-" + slug
	const resource = "https://member.example.com/mcp"
	grant := jwtGrant{subject: "user-123", scope: "jwt:read", scopeInToken: true}
	_, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, grant, grant),
		withIDTokenIssuer(issuer), withResource(resource), withResourceIndicatorSupported(false))

	require.Equal(t, resource, env.session.Resource.String, "the session still records the resource for routing")
	require.Equal(t, remotesessions.IdentitySourceJWTAccessToken, env.session.IdentitySource.String)
	require.Equal(t, "user-123", env.session.UpstreamSubject.String)
	require.Equal(t, "ok", decodeEnrichment(t, env.session.Enrichment).Interfaces["jwt_access_token"].Status)
}

func TestJWTAccessTokenExchangeDefersToUserInfo(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"userinfo@example.com"}`), nil)
	const slug = "jwt-exchange-userinfo-priority"
	clientID := "synthetic-cid-" + slug
	grant := jwtGrant{subject: "user-123", scope: "jwt:read", scopeInToken: true, responseScope: new("response:read")}
	ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, grant, grant),
		withIDTokenIssuer(issuer), withEnrichmentAS(as))

	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String)
	require.Equal(t, "userinfo@example.com", env.session.UpstreamEmail.String)
	require.NotContains(t, string(env.session.Enrichment), "jwt_access_token")
	require.Zero(t, issuer.fetches.Load(), "successful UserInfo avoids a lower-ranked JWT key fetch")
	_, err := env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	require.Zero(t, issuer.fetches.Load(), "Verify skips JWT enrichment behind stored UserInfo")
}

func TestJWTAccessTokenExchangeDefersToIDToken(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const slug = "jwt-exchange-id-token-priority"
	const clientID = "synthetic-cid-" + slug
	var nonce atomic.Pointer[string]
	handler := func(w http.ResponseWriter, _ *http.Request) {
		accessClaims := accessTokenClaims(issuer.issuerURL, clientID)
		accessClaims["client_id"] = clientID
		access := mintAccessToken(t, issuer, new("at+jwt"), accessClaims)
		idToken := issuer.mint(t, issuer.claims(clientID, loadString(&nonce)))
		raw, err := json.Marshal(map[string]any{"access_token": access, "refresh_token": "refresh-token", "token_type": "Bearer", "expires_in": 3600, "scope": "response:scope", "id_token": idToken})
		if !assert.NoError(t, err) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
	_, env := newSyntheticExpiryEnv(t, slug, handler, withIDTokenIssuer(issuer), observeNonce(&nonce))
	require.Equal(t, remotesessions.IdentitySourceIDToken, env.session.IdentitySource.String)
	require.Contains(t, string(env.session.Enrichment), "id_token")
	require.NotContains(t, string(env.session.Enrichment), "jwt_access_token")
}

func TestJWTAccessTokenRefreshOrchestration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		refreshed    jwtGrant
		wantIdentity bool
		wantScopes   []string
	}{
		{name: "matching JWT restores identity and scope", refreshed: jwtGrant{subject: "user-123", scope: "jwt:new", scopeInToken: true}, wantIdentity: true, wantScopes: []string{"jwt:new"}},
		{name: "mismatched JWT leaves identity retired", refreshed: jwtGrant{subject: "other-user", scope: "jwt:new", scopeInToken: true}, wantScopes: []string{"jwt:old"}},
		{name: "opaque replacement leaves identity retired", refreshed: jwtGrant{opaque: true}, wantScopes: []string{"jwt:old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issuer := newIDTokenIssuer(t)
			slug := "jwt-refresh-" + strings.NewReplacer(" ", "-").Replace(tc.name)
			clientID := "synthetic-cid-" + slug
			initial := jwtGrant{subject: "user-123", scope: "jwt:old", scopeInToken: true}
			ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, initial, tc.refreshed), withIDTokenIssuer(issuer))

			result, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
			require.NoError(t, err)
			stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			require.Equal(t, result.Session.ID, stored.ID)
			require.Equal(t, tc.wantScopes, stored.Scopes)
			require.False(t, stored.LastValidatedAt.Valid)
			require.False(t, stored.ValidationStatus.Valid)
			if tc.wantIdentity {
				require.Equal(t, remotesessions.IdentitySourceJWTAccessToken, stored.IdentitySource.String)
				require.Equal(t, "user-123", stored.UpstreamSubject.String)
				require.Contains(t, string(stored.Enrichment), "jwt_access_token")
			} else {
				require.False(t, stored.IdentitySource.Valid)
				require.False(t, stored.UpstreamSubject.Valid)
			}
		})
	}
}

func TestJWTAccessTokenRefreshDefersToStoredUserInfo(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"userinfo@example.com"}`), nil)
	const slug = "jwt-refresh-userinfo-priority"
	clientID := "synthetic-cid-" + slug
	grant := jwtGrant{subject: "user-123", scope: "jwt:scope", scopeInToken: true, responseScope: new("response:scope")}
	ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, grant, grant),
		withIDTokenIssuer(issuer), withEnrichmentAS(as), withClientScope("stored:scope"))
	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String)

	_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.Equal(t, remotesessions.IdentitySourceUserinfo, stored.IdentitySource.String)
	require.Equal(t, "userinfo@example.com", stored.UpstreamEmail.String)
	require.NotContains(t, string(stored.Enrichment), "jwt_access_token")
	require.Zero(t, issuer.fetches.Load(), "stored UserInfo avoids lower-ranked refresh JWT verification")
}

func TestJWTAccessTokenRefreshRecoversScopeBehindStoredUserInfo(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"userinfo@example.com"}`), nil)
	const slug = "jwt-refresh-userinfo-scope"
	clientID := "synthetic-cid-" + slug
	initial := jwtGrant{subject: "user-123", responseScope: new("initial:scope")}
	refreshed := jwtGrant{subject: "user-123", scope: "jwt:new", scopeInToken: true}
	ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, initial, refreshed),
		withIDTokenIssuer(issuer), withEnrichmentAS(as))
	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String)

	_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.Equal(t, remotesessions.IdentitySourceUserinfo, stored.IdentitySource.String)
	require.Equal(t, "userinfo@example.com", stored.UpstreamEmail.String)
	require.Equal(t, []string{"jwt:new"}, stored.Scopes)
	require.Positive(t, issuer.fetches.Load(), "omitted response scope requires verified JWT metadata")
}

func TestJWTAccessTokenRefreshFailureRecordSurvives(t *testing.T) {
	t.Parallel()

	issuer := newIDTokenIssuer(t)
	const slug = "jwt-refresh-failure-record"
	clientID := "synthetic-cid-" + slug
	initial := jwtGrant{subject: "user-123"}
	ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, initial, jwtGrant{subject: "other-user"}), withIDTokenIssuer(issuer))
	_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.Contains(t, string(stored.Enrichment), `"jwt_access_token"`)
	require.Contains(t, string(stored.Enrichment), `"subject mismatch"`)
}

type hookJWKSCache struct {
	inner jwks.Cache
	armed atomic.Bool
	hook  func(context.Context)
}

func (c *hookJWKSCache) Get(ctx context.Context, key string) (jwks.CacheState, error) {
	state, err := c.inner.Get(ctx, key)
	if err == nil && c.armed.Swap(false) && c.hook != nil {
		c.hook(ctx)
	}
	if err != nil {
		return state, fmt.Errorf("get cached JWKS: %w", err)
	}
	return state, nil
}

func (c *hookJWKSCache) Put(ctx context.Context, key string, state jwks.CacheState) error {
	if err := c.inner.Put(ctx, key, state); err != nil {
		return fmt.Errorf("put cached JWKS: %w", err)
	}
	return nil
}

func TestJWTAccessTokenRefreshCASRejectsStaleRestatement(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"reconnect", "revoke"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			issuer := newIDTokenIssuer(t)
			slug := "jwt-refresh-cas-" + action
			clientID := "synthetic-cid-" + slug
			cache := &hookJWKSCache{inner: jwks.NewMemoryCache()}
			grant := jwtGrant{subject: "user-123", scope: "jwt:scope", scopeInToken: true}
			ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, grant, grant),
				withIDTokenIssuer(issuer), withIDTokenKeyCache(cache))

			var hookErr atomic.Pointer[error]
			cache.hook = func(hookCtx context.Context) {
				if action == "revoke" {
					_, err := env.q.SoftDeleteRemoteSessionsByClientID(hookCtx, env.clientID)
					if err != nil {
						hookErr.Store(&err)
					}
					return
				}
				current, err := env.q.GetActiveRemoteSession(hookCtx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
				if err != nil {
					hookErr.Store(&err)
					return
				}
				_, err = env.q.UpsertRemoteSession(hookCtx, repo.UpsertRemoteSessionParams{
					SubjectUrn: env.subject, UserSessionIssuerID: current.UserSessionIssuerID, RemoteSessionClientID: env.clientID,
					AccessTokenEncrypted: current.AccessTokenEncrypted, AccessExpiresAt: current.AccessExpiresAt,
					RefreshTokenEncrypted: current.RefreshTokenEncrypted, AuthorizationExpiresAt: current.AuthorizationExpiresAt, RefreshExpiresAt: current.RefreshExpiresAt,
					Scopes: current.Scopes, Resource: current.Resource, AutoRefresh: current.AutoRefresh,
					UpstreamSubject: conv.ToPGText("reconnected-user"), UpstreamEmail: conv.ToPGText("reconnected@example.com"),
					UpstreamDisplayName: conv.ToPGText("Reconnected"), IdentitySource: conv.ToPGText(remotesessions.IdentitySourceUserinfo),
					Enrichment: []byte(`{"userinfo":{"sub":"reconnected-user"}}`),
				})
				if err != nil {
					hookErr.Store(&err)
				}
			}
			cache.armed.Store(true)
			_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
			require.NoError(t, err)
			require.Nil(t, hookErr.Load())
			require.False(t, cache.armed.Load(), "the row moved during JWT verification")

			if action == "reconnect" {
				stored, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
				require.NoError(t, err)
				require.Equal(t, remotesessions.IdentitySourceUserinfo, stored.IdentitySource.String)
				require.Equal(t, "reconnected@example.com", stored.UpstreamEmail.String)
				require.NotContains(t, string(stored.Enrichment), "jwt_access_token")
			} else {
				tombstone, err := env.q.GetRemoteSessionByIDIncludingDeleted(ctx, repo.GetRemoteSessionByIDIncludingDeletedParams{ID: env.session.ID, ProjectID: conv.ToNullUUID(env.projectID)})
				require.NoError(t, err)
				require.True(t, tombstone.Deleted)
				require.False(t, tombstone.IdentitySource.Valid)
			}
		})
	}
}

func TestJWTAccessTokenPreservesRejectedIDTokenMarker(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	const slug = "jwt-rejected-id-marker"
	const clientID = "synthetic-cid-" + slug
	claims := accessTokenClaims(issuer.issuerURL, clientID)
	claims["client_id"] = clientID
	body := map[string]any{
		"access_token":  mintAccessToken(t, issuer, new("at+jwt"), claims),
		"refresh_token": "example-refresh", "token_type": "Bearer", "expires_in": 3600,
		"id_token": "invalid-id-token",
	}
	exchange, err := json.Marshal(body)
	require.NoError(t, err)
	delete(body, "id_token")
	refresh, err := json.Marshal(body)
	require.NoError(t, err)
	ctx, env := newSyntheticExpiryEnv(t, slug, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("grant_type") == "refresh_token" {
			_, _ = w.Write(refresh)
		} else {
			_, _ = w.Write(exchange)
		}
	}, withIDTokenIssuer(issuer))
	require.False(t, env.session.IdentitySource.Valid)
	require.Equal(t, "rejected", decodeEnrichment(t, env.session.Enrichment).Interfaces["id_token"].Status)
	_, err = env.mgr.EnrichRemoteSession(ctx, env.ref(env.session))
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.False(t, sess.IdentitySource.Valid)
	require.NotContains(t, string(sess.Enrichment), "jwt_access_token")
	_, err = env.refresher.RefreshNow(ctx, sess, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	sess = reloadSession(t, env)
	require.False(t, sess.IdentitySource.Valid)
	require.NotContains(t, string(sess.Enrichment), "jwt_access_token")
	require.Equal(t, "rejected", decodeEnrichment(t, sess.Enrichment).Interfaces["id_token"].Status)
	require.Zero(t, issuer.fetches.Load(), "a rejected grant never tries a weaker JWT identity")
}

func TestJWTAccessTokenConsentKeepsProviderContextWithoutIdentityLabel(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	const slug = "jwt-context-without-label"
	const clientID = "synthetic-cid-" + slug
	claims := accessTokenClaims(issuer.issuerURL, clientID)
	claims["client_id"] = clientID
	delete(claims, "email")
	body, err := json.Marshal(map[string]any{
		"access_token": mintAccessToken(t, issuer, new("at+jwt"), claims),
		"token_type":   "Bearer", "expires_in": 3600, "workspace_name": "Example workspace",
	})
	require.NoError(t, err)
	ctx, env := newSyntheticExpiryEnv(t, slug, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}, withIDTokenIssuer(issuer))
	require.Equal(t, remotesessions.IdentitySourceJWTAccessToken, env.session.IdentitySource.String)
	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	state, ok := states[env.clientID]
	require.True(t, ok)
	require.Empty(t, state.ConnectedAs, "an opaque subject and name alone do not label the account")
	require.Equal(t, []string{"Example workspace"}, state.AccountChips)
}

// A JWT that only supplies omitted scopes still leaves its sanitized claims
// under enrichment.jwt_access_token beside the stronger stored identity.
func TestJWTAccessTokenScopeRecoveryKeepsClaimsBesideStrongerIdentity(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	as := newEnrichmentAS(t)
	as.script(jsonHandler(http.StatusOK, `{"sub":"user-123","email":"userinfo@example.com"}`), nil)
	const slug = "jwt-scope-keeps-claims"
	clientID := "synthetic-cid-" + slug
	initial := jwtGrant{subject: "user-123", responseScope: new("initial:scope")}
	refreshed := jwtGrant{subject: "user-123", scope: "jwt:new", scopeInToken: true}
	ctx, env := newSyntheticExpiryEnv(t, slug, jwtGrantHandler(t, issuer, clientID, initial, refreshed),
		withIDTokenIssuer(issuer), withEnrichmentAS(as))
	require.Equal(t, remotesessions.IdentitySourceUserinfo, env.session.IdentitySource.String)

	_, err := env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.Equal(t, remotesessions.IdentitySourceUserinfo, sess.IdentitySource.String)
	require.Equal(t, []string{"jwt:new"}, sess.Scopes)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(sess.Enrichment, &doc))
	require.Contains(t, string(doc["jwt_access_token"]), `"user-123"`)
	require.Equal(t, "ok", decodeEnrichment(t, sess.Enrichment).Interfaces["jwt_access_token"].Status)
}

// A refresh whose ID token verifies supersedes the exchange-time rejection, so
// the JWT may recover omitted scopes for the grant again.
func TestJWTAccessTokenVerifiedRefreshIDTokenLiftsRejectedMarker(t *testing.T) {
	t.Parallel()
	issuer := newIDTokenIssuer(t)
	const slug = "jwt-rejected-marker-lifted"
	const clientID = "synthetic-cid-" + slug
	claims := accessTokenClaims(issuer.issuerURL, clientID)
	claims["client_id"] = clientID
	claims["scope"] = "jwt:new"
	body := map[string]any{
		"access_token":  mintAccessToken(t, issuer, new("at+jwt"), claims),
		"refresh_token": "example-refresh", "token_type": "Bearer", "expires_in": 3600,
		"id_token": "invalid-id-token",
	}
	exchange, err := json.Marshal(body)
	require.NoError(t, err)
	body["id_token"] = issuer.mint(t, issuer.claims(clientID, ""))
	refresh, err := json.Marshal(body)
	require.NoError(t, err)
	ctx, env := newSyntheticExpiryEnv(t, slug, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("grant_type") == "refresh_token" {
			_, _ = w.Write(refresh)
		} else {
			_, _ = w.Write(exchange)
		}
	}, withIDTokenIssuer(issuer), withIssuerScopes("initial:scope"))
	require.False(t, env.session.IdentitySource.Valid)
	require.Equal(t, []string{"initial:scope"}, env.session.Scopes, "a rejected grant recovers no scope at exchange")

	_, err = env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
	require.NoError(t, err)
	sess := reloadSession(t, env)
	require.Equal(t, remotesessions.IdentitySourceIDToken, sess.IdentitySource.String)
	require.Equal(t, []string{"jwt:new"}, sess.Scopes, "a verified ID token lets the JWT recover the omitted scope")
	require.NotEqual(t, "rejected", decodeEnrichment(t, sess.Enrichment).Interfaces["id_token"].Status)
}
