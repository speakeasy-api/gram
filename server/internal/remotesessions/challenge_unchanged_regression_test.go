// Pins the exact wire behaviour of the remote-login dance for issuers #6109's rules do not touch.

package remotesessions_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const unchangedResource = "https://member.example.com/mcp"

// authorizeQuery parses the query of an authorize redirect.
func authorizeQuery(t *testing.T, authURL string) url.Values {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	return parsed.Query()
}

// sortedKeys is the exact parameter set of a query or form.
func sortedKeys(v url.Values) []string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// exchangeSpy records every token-endpoint POST body and answers with a
// scopeless token, or with the given scope when non-empty.
type exchangeSpy struct {
	mu    sync.Mutex
	forms []url.Values
	auth  []string
	scope string
}

func (s *exchangeSpy) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.forms = append(s.forms, r.PostForm)
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()
		if s.scope != "" {
			scopedToken(s.scope)(w, r)
			return
		}
		scopelessToken(w, r)
	}
}

func (s *exchangeSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.forms)
}

func (s *exchangeSpy) only(t *testing.T) (url.Values, string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.forms, 1, "exactly one code exchange")
	return s.forms[0], s.auth[0]
}

// Authorize params for an issuer advertising no standard scopes are exactly as before.
func TestRemoteLogin_Unchanged_AuthorizeURLForIssuerWithoutStandardScopes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		slug         string
		issuerScopes []string
		clientScope  []string
		wantScope    string
		wantKeys     []string
	}{
		{
			name:         "client scope is requested verbatim in stored order",
			slug:         "unchanged-client-scope",
			issuerScopes: []string{"read", "write", "admin"},
			clientScope:  []string{"write:tools", "read:tools"},
			wantScope:    "write:tools read:tools",
			wantKeys:     []string{"client_id", "code_challenge", "code_challenge_method", "redirect_uri", "resource", "response_type", "scope", "state"},
		},
		{
			name:         "no client scope requests scopes_supported verbatim in stored order",
			slug:         "unchanged-issuer-scope",
			issuerScopes: []string{"write", "read"},
			clientScope:  nil,
			wantScope:    "write read",
			wantKeys:     []string{"client_id", "code_challenge", "code_challenge_method", "redirect_uri", "resource", "response_type", "scope", "state"},
		},
		{
			name:         "no client scope and empty scopes_supported sends no scope parameter",
			slug:         "unchanged-no-scope",
			issuerScopes: []string{},
			clientScope:  nil,
			wantScope:    "",
			wantKeys:     []string{"client_id", "code_challenge", "code_challenge_method", "redirect_uri", "resource", "response_type", "state"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, env := newSyntheticExpiryEnv(t, tc.slug, scopelessToken,
				withIssuerScopes(tc.issuerScopes...),
				withClientScope(tc.clientScope...),
				withResource(unchangedResource),
			)
			q := authorizeQuery(t, env.authURL)

			require.Equal(t, tc.wantKeys, sortedKeys(q), "the parameter set is exactly what it was")
			require.Equal(t, tc.wantScope, q.Get("scope"))
			require.Equal(t, unchangedResource, q.Get("resource"))
			require.Equal(t, "code", q.Get("response_type"))
			require.Equal(t, "synthetic-cid-"+tc.slug, q.Get("client_id"))
			require.Equal(t, "http://localhost/mcp/remote_login_callback", q.Get("redirect_uri"))
			require.Equal(t, "S256", q.Get("code_challenge_method"))
			require.NotEmpty(t, q.Get("code_challenge"))
			require.NotEmpty(t, q.Get("state"))
			require.False(t, q.Has("nonce"), "a non-OIDC issuer never got a nonce")
			require.False(t, q.Has("audience"), "no audience is configured")
		})
	}
}

// The code exchange body is exactly as before, with no scope.
func TestRemoteLoginCallback_Unchanged_CodeExchangeBody(t *testing.T) {
	t.Parallel()

	spy := &exchangeSpy{}
	_, env := newSyntheticExpiryEnv(t, "unchanged-exchange", spy.handler(),
		withIssuerScopes("read", "write"),
		withClientScope("read"),
		withResource(unchangedResource),
	)
	form, auth := spy.only(t)

	require.Equal(t, []string{"client_id", "code", "code_verifier", "grant_type", "redirect_uri", "resource"}, sortedKeys(form))
	require.Equal(t, "authorization_code", form.Get("grant_type"))
	require.Equal(t, "upstream-code", form.Get("code"))
	require.Equal(t, unchangedResource, form.Get("resource"))
	require.Equal(t, "synthetic-cid-unchanged-exchange", form.Get("client_id"))
	require.Empty(t, auth, "the none auth method sends no Authorization header")

	authorize := authorizeQuery(t, env.authURL)
	require.Equal(t, authorize.Get("redirect_uri"), form.Get("redirect_uri"))
	sum := sha256.Sum256([]byte(form.Get("code_verifier")))
	require.Equal(t, authorize.Get("code_challenge"), base64.RawURLEncoding.EncodeToString(sum[:]), "the verifier is the one behind the S256 challenge")

	require.Equal(t, unchangedResource, env.session.Resource.String)
}

// The refresh grant body is exactly as before while the flag is NULL.
func TestRefreshGrant_Unchanged_ResourceSentWhenIndicatorSupportUnlearned(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "unchanged-refresh", pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, clientID)
	require.NoError(t, err)
	require.False(t, client.ResourceIndicatorSupported.Valid, "the issuer's flag starts NULL")

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, unchangedResource)
	require.NoError(t, err)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, "refreshed-access", tok)

	require.Equal(t, []string{"client_id", "client_secret", "grant_type", "refresh_token", "resource"}, sortedKeys(spy.form))
	require.Equal(t, "refresh_token", spy.form.Get("grant_type"))
	require.Equal(t, unchangedResource, spy.form.Get("resource"))
	require.Equal(t, "aud-cid", spy.form.Get("client_id"))
	require.Empty(t, spy.authHdr)

	flag := issuerResourceIndicatorFlag(t, ctx, q, client.RemoteSessionIssuerID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	require.False(t, flag.Valid, "a refresh teaches nothing about the flag")
}

// Without the iss capability the callback ignores a missing or wrong iss, as before.
func TestRemoteLoginCallback_Unchanged_IssNotRequiredWhenIssuerDoesNotAdvertiseIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		slug string
		opts []syntheticLoginOption
	}{
		{name: "NULL and no iss", slug: "unchanged-iss-null-absent", opts: nil},
		{name: "NULL and wrong iss", slug: "unchanged-iss-null-wrong", opts: []syntheticLoginOption{
			withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") }),
		}},
		{name: "false and no iss", slug: "unchanged-iss-false-absent", opts: []syntheticLoginOption{
			withIssParameterSupported(false),
		}},
		{name: "false and wrong iss", slug: "unchanged-iss-false-wrong", opts: []syntheticLoginOption{
			withIssParameterSupported(false),
			withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") }),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			spy := &exchangeSpy{}
			opts := append([]syntheticLoginOption{withResource(unchangedResource)}, tc.opts...)
			ctx, env, w, err := driveSyntheticLogin(t, tc.slug, spy.handler(), opts...)
			require.NoError(t, err)
			require.Equal(t, http.StatusSeeOther, w.Code)
			require.True(t, strings.HasPrefix(w.Header().Get("Location"), "http://localhost/mcp/synthetic-mcp-"+tc.slug+"/connect?state="), w.Header().Get("Location"))
			require.Equal(t, 1, spy.count())

			session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
				SubjectUrn:            env.subject,
				RemoteSessionClientID: env.clientID,
			})
			require.NoError(t, err)
			require.Equal(t, unchangedResource, session.Resource.String)
		})
	}
}

// A response scope is stored verbatim: order, extras, narrower, or equal.
func TestRemoteLoginCallback_Unchanged_ResponseScopeStoredVerbatim(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		slug          string
		responseScope string
		wantScopes    []string
	}{
		{name: "same set in the provider's order", slug: "unchanged-scope-reordered", responseScope: "read write", wantScopes: []string{"read", "write"}},
		{name: "a scope that was never requested", slug: "unchanged-scope-extra", responseScope: "write read extra", wantScopes: []string{"write", "read", "extra"}},
		{name: "a narrower set", slug: "unchanged-scope-narrower", responseScope: "read", wantScopes: []string{"read"}},
		{name: "the requested set", slug: "unchanged-scope-equal", responseScope: "write read", wantScopes: []string{"write", "read"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, env := newSyntheticExpiryEnv(t, tc.slug, scopedToken(tc.responseScope),
				withIssuerScopes("read", "write"),
				withClientScope("write", "read"),
			)
			require.Equal(t, "write read", authorizeQuery(t, env.authURL).Get("scope"))
			require.Equal(t, tc.wantScopes, env.session.Scopes)
		})
	}
}

// A client scope already carrying the standard scopes is sent as stored, nothing widened.
func TestRemoteLogin_Unchanged_ClientScopeAlreadyCarryingStandardScopes(t *testing.T) {
	t.Parallel()

	stored := []string{"offline_access", "read", "openid", "profile", "email"}
	ctx, env := newSyntheticExpiryEnv(t, "unchanged-standard-stored", scopelessToken,
		withIssuerScopes("openid", "email", "profile", "offline_access", "read"),
		withClientScope(stored...),
	)
	require.Equal(t, strings.Join(stored, " "), authorizeQuery(t, env.authURL).Get("scope"))
	require.Equal(t, stored, env.session.Scopes)

	clients, err := env.mgr.ListClients(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	scopes, widened := clients[0].RequestedScopes()
	require.Equal(t, stored, scopes)
	require.Empty(t, widened, "nothing was appended, so nothing is logged as widened")
}

// access_denied on a valid state is refused as before. One change is pinned:
// the denial now consumes the login state, so a later code on it cannot complete.
func TestRemoteLoginCallback_Unchanged_AccessDeniedWithValidState(t *testing.T) {
	t.Parallel()

	spy := &exchangeSpy{}
	ctx, env, w, err := driveSyntheticLogin(t, "unchanged-denied", spy.handler(),
		withResource(unchangedResource),
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "access_denied")
			q.Set("error_description", "The user declined")
		}),
	)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	require.Equal(t, "remote authn challenge denied: access_denied", shareable.Error())
	require.Empty(t, w.Header().Get("Location"), "a denial never redirects")
	require.Equal(t, 0, spy.count(), "a denial never exchanges a code")

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "no session is stored for a denied login")

	// Changed in #6109: the denial consumed the state, so a code on the same
	// state is no longer exchangeable.
	second, err := env.callback(t, "code=upstream-code&state="+url.QueryEscape(stateOf(t, env.authURL)))
	require.Error(t, err)
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, "remote login state not found or expired", shareable.Error())
	require.Empty(t, second.Header().Get("Location"))
	require.Equal(t, 0, spy.count())
}

// legacyRemoteLoginState is the pre-#6109 serialisation, stored through the manager's own cache adapter.
type legacyRemoteLoginState struct {
	ID                    string              `json:"id"`
	ParentChallengeID     string              `json:"parent_challenge_id"`
	ProjectID             uuid.UUID           `json:"project_id"`
	OrganizationID        string              `json:"organization_id,omitempty"`
	UserSessionIssuerID   uuid.UUID           `json:"user_session_issuer_id"`
	RemoteSessionClientID uuid.UUID           `json:"remote_session_client_id"`
	TokenEndpoint         string              `json:"token_endpoint"`
	RedirectURI           string              `json:"redirect_uri"`
	CodeVerifier          string              `json:"code_verifier"`
	Resource              string              `json:"resource,omitempty"`
	Subject               *urn.SessionSubject `json:"subject,omitempty"`
	McpSlug               string              `json:"mcp_slug"`
	RouteBase             string              `json:"route_base,omitempty"`
	FinalRedirectURI      string              `json:"final_redirect_uri,omitempty"`
	AutoRefresh           *bool               `json:"auto_refresh,omitempty"`
	CreatedAt             time.Time           `json:"created_at"`
}

// A pre-#6109 state still completes: exchange with resource, empty scopes.
func TestRemoteLoginCallback_Unchanged_LegacyLoginStateCompletes(t *testing.T) {
	t.Parallel()

	const slug = "unchanged-legacy-state"
	spy := &exchangeSpy{}
	ctx, env := newSyntheticExpiryEnv(t, slug, spy.handler(),
		withIssuerScopes("read", "write"),
		withClientScope("read"),
		withResource(unchangedResource),
	)
	require.Equal(t, 1, spy.count())

	issuer, err := env.q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    env.issuerID,
		ProjectID:             conv.ToNullUUID(env.projectID),
		OrganizationID:        conv.ToPGText(env.organizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
	})
	require.NoError(t, err)

	legacySubject := urn.NewUserSubject("legacy-subject-" + slug)
	stateID := uuid.NewString()
	parentID := uuid.NewString()
	legacy := legacyRemoteLoginState{
		ID:                    stateID,
		ParentChallengeID:     parentID,
		ProjectID:             env.projectID,
		OrganizationID:        env.organizationID,
		UserSessionIssuerID:   env.session.UserSessionIssuerID,
		RemoteSessionClientID: env.clientID,
		TokenEndpoint:         issuer.TokenEndpoint.String,
		RedirectURI:           authorizeQuery(t, env.authURL).Get("redirect_uri"),
		CodeVerifier:          "legacy-verifier",
		Resource:              unchangedResource,
		Subject:               &legacySubject,
		McpSlug:               "synthetic-mcp-" + slug,
		RouteBase:             "",
		FinalRedirectURI:      "",
		AutoRefresh:           nil,
		CreatedAt:             time.Now(),
	}

	// The typed cache key is CacheKey() + ":" + SuffixNone on Redis db 0.
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	adapter := cache.NewRedisCacheAdapter(redisClient)
	require.NoError(t, adapter.Set(ctx, "remoteLogin:"+stateID+":", legacy, 10*time.Minute))

	var decoded remotesessions.RemoteLoginState
	require.NoError(t, adapter.Get(ctx, "remoteLogin:"+stateID+":", &decoded), "the legacy payload decodes into the current struct")
	require.Equal(t, stateID, decoded.ID)
	require.Empty(t, decoded.Scopes)
	require.False(t, decoded.OmitResource)
	require.Empty(t, decoded.ExpectedIssuer)

	w, err := env.callback(t, "code=legacy-code&state="+url.QueryEscape(stateID))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, w.Code)
	require.Equal(t, "http://localhost/mcp/synthetic-mcp-"+slug+"/connect?state="+parentID, w.Header().Get("Location"))

	require.Equal(t, 2, spy.count())
	spy.mu.Lock()
	form := spy.forms[1]
	spy.mu.Unlock()
	require.Equal(t, []string{"client_id", "code", "code_verifier", "grant_type", "redirect_uri", "resource"}, sortedKeys(form))
	require.Equal(t, "legacy-code", form.Get("code"))
	require.Equal(t, "legacy-verifier", form.Get("code_verifier"))
	require.Equal(t, unchangedResource, form.Get("resource"))

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            legacySubject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, unchangedResource, session.Resource.String)
	require.Empty(t, session.Scopes, "a pre-#6109 state has no requested set to record")

	require.Error(t, adapter.Get(ctx, "remoteLogin:"+stateID+":", &decoded), "the state is single-use")
}
