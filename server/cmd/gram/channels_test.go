package gram

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/channels"
	"github.com/speakeasy-api/gram/server/internal/channels/authn"
	channelsrepo "github.com/speakeasy-api/gram/server/internal/channels/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var channelsTestProjectID = uuid.MustParse("e9b998fd-017e-42db-9bd1-bc49e3c882da")

type channelsTestTokens struct{ calls int }

func (a *channelsTestTokens) Authenticate(ctx context.Context, _ string) (authn.Identity, error) {
	a.calls++
	target, _ := authn.TargetFromContext(ctx)
	return authn.Identity{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "user_example"), OrganizationID: "org_example", ProjectID: target.ProjectID}, nil
}

type channelsTestSessions struct {
	calls        int
	email        string
	unvalidated  bool
	actorID      string
	support      bool
	impersonated bool
}

func (a *channelsTestSessions) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	a.calls++
	auth := &contextvalues.AuthContext{UserID: "user_example", ActiveOrganizationID: "org_example"}
	if a.email != "" {
		auth.Email = conv.PtrEmpty(a.email)
	}
	if a.unvalidated {
		return contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, "user_example")), nil
	}
	ctx = contextvalues.WithValidatedGramSession(ctx, auth, a.impersonated)
	auth, _ = contextvalues.GetAuthContext(ctx)
	if a.support {
		auth.IsAdmin = true
		auth.SupportOrganizationID = auth.ActiveOrganizationID
		ctx = contextvalues.WithValidatedSupportSession(ctx, auth)
	}
	if a.actorID != "" {
		ctx = contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, a.actorID))
	}
	return ctx, nil
}

type channelsTestNames struct {
	calls       int
	displayName string
	err         error
}

func (a *channelsTestNames) ChannelsDisplayName(context.Context, string) (string, error) {
	a.calls++
	return a.displayName, a.err
}

type channelsTestPreparer struct {
	calls     int
	principal authn.Identity
}

func (a *channelsTestPreparer) PrepareContext(ctx context.Context, principal authn.Identity) (context.Context, error) {
	a.calls++
	a.principal = principal
	return ctx, errors.New("revoked")
}

func TestChannelsAuthenticatorRevalidatesBearerEveryCall(t *testing.T) {
	t.Parallel()
	tokens, preparer := &channelsTestTokens{}, &channelsTestPreparer{}
	authenticate := newChannelsAuthenticator(channelsTestConfig(t), tokens, nil, preparer, nil)
	request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/mcp", nil)
	request.Header.Set("Authorization", "bEaReR example")
	for range 2 {
		_, err := authenticate(request)
		require.ErrorContains(t, err, "revoked")
	}
	require.Equal(t, 2, tokens.calls)
	require.Equal(t, 2, preparer.calls)
}

func TestChannelsDashboardAPIRejectsBearer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, body string
	}{
		{name: "snapshot", method: http.MethodGet},
		{name: "dashboard session", method: http.MethodPost, body: `{"operation":"channels_dashboard_session","arguments":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tokens, sessions := &channelsTestTokens{}, &channelsTestSessions{}
			handler := channels.New(nil, newChannelsAuthenticator(channelsTestConfig(t), tokens, sessions, channelsAdmittedPreparer{}, nil))
			request := httptest.NewRequest(tc.method, "/channels/oauth-test/default/api", strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer example")
			request.Header.Set("Origin", "https://app.example.test")
			request.AddCookie(&http.Cookie{Name: "session", Value: "example"})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.Zero(t, tokens.calls)
			require.Zero(t, sessions.calls)
		})
	}
}

func TestChannelsAuthenticatorAcceptsDashboardCookieAndMCPBearer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		bearer     bool
	}{
		{name: "dashboard cookie", path: "/channels/oauth-test/default/api"},
		{name: "MCP bearer", path: "/channels/oauth-test/default/mcp", bearer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tokens, sessions := &channelsTestTokens{}, &channelsTestSessions{}
			config := channelsTestConfig(t)
			authenticate := newChannelsAuthenticator(config, tokens, sessions, channelsAdmittedPreparer{}, nil)
			request := httptest.NewRequest(http.MethodPost, tc.path, nil)
			request.Header.Set("Origin", "https://app.example.test")
			if tc.bearer {
				request.Header.Set("Authorization", "Bearer example")
			} else {
				request.AddCookie(&http.Cookie{Name: "session", Value: "example"})
			}
			principal, err := authenticate(request)
			require.NoError(t, err)
			require.Equal(t, "user_example", principal.SubjectID)
			require.Equal(t, "org_example", principal.OrganizationID)
			require.Equal(t, channelsTestProjectID.String(), principal.ProjectID)
			if tc.bearer {
				require.Equal(t, 1, tokens.calls)
				require.Zero(t, sessions.calls)
			} else {
				require.Zero(t, tokens.calls)
				require.Equal(t, 1, sessions.calls)
			}
		})
	}
}

func TestChannelsAuthenticatorBrowserSessionBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, origin string
		sessionCalls       int
	}{
		{name: "mcp rejects cookies", path: "/channels/oauth-test/default/mcp", origin: "https://app.example.test"},
		{name: "api rejects foreign origin", path: "/channels/oauth-test/default/api", origin: "https://other.example.test"},
		{name: "api rejects missing origin", path: "/channels/oauth-test/default/api"},
		{name: "api accepts dashboard origin", path: "/channels/oauth-test/default/api", origin: "https://app.example.test", sessionCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions, preparer := &channelsTestSessions{}, &channelsTestPreparer{}
			authenticate := newChannelsAuthenticator(channelsTestConfig(t), nil, sessions, preparer, nil)
			request := httptest.NewRequest(http.MethodPost, tc.path, nil)
			request.Header.Set("Origin", tc.origin)
			_, err := authenticate(request)
			require.Error(t, err)
			require.Equal(t, tc.sessionCalls, sessions.calls)
			require.Equal(t, tc.sessionCalls, preparer.calls)
			if tc.sessionCalls != 0 {
				require.Equal(t, "org_example", preparer.principal.OrganizationID)
				require.Equal(t, "user_example", preparer.principal.Principal.ID)
				require.Equal(t, urn.PrincipalTypeUser, preparer.principal.Principal.Type)
			}
		})
	}
}

// A dashboard session is labelled from the account, and the label is published
// to every authorized member and to the MCP clients they have connected. It
// must not be a full email address.
func TestChannelsAuthenticatorLabelsSessionsWithoutPublishingEmailAddresses(t *testing.T) {
	t.Parallel()
	config := channelsTestConfig(t)

	for _, tc := range []struct {
		name        string
		displayName string
		nameErr     error
		email       string
		want        string
		nameCalls   int
	}{
		{name: "display name preferred", displayName: "Example Person", email: "person@example.test", want: "Example Person", nameCalls: 1},
		{name: "display name trimmed", displayName: "  Example Person  ", email: "person@example.test", want: "Example Person", nameCalls: 1},
		{name: "no display name falls back to the local part", email: "person@example.test", want: "person", nameCalls: 1},
		{name: "lookup failure falls back to the local part", nameErr: errors.New("unavailable"), email: "person@example.test", want: "person", nameCalls: 1},
		{name: "no name and no address stays empty", nameCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions := &channelsTestSessions{email: tc.email}
			names := &channelsTestNames{displayName: tc.displayName, err: tc.nameErr}
			authenticate := newChannelsAuthenticator(config, nil, sessions, channelsAdmittedPreparer{}, names)
			request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/api", nil)
			request.Header.Set("Origin", "https://app.example.test")
			principal, err := authenticate(request)
			require.NoError(t, err)
			require.Equal(t, tc.want, principal.SubjectName)
			require.NotContains(t, principal.SubjectName, "@")
			require.Equal(t, tc.nameCalls, names.calls)
		})
	}
}

// Reading the snapshot opens no session, so it resolves no name and costs no
// account lookup on every poll.
func TestChannelsAuthenticatorResolvesNoNameForReads(t *testing.T) {
	t.Parallel()
	sessions := &channelsTestSessions{email: "person@example.test"}
	names := &channelsTestNames{displayName: "Example Person"}
	authenticate := newChannelsAuthenticator(channelsTestConfig(t), nil, sessions, channelsAdmittedPreparer{}, names)
	principal, err := authenticate(httptest.NewRequest(http.MethodGet, "/channels/oauth-test/default/api", nil))
	require.NoError(t, err)
	require.Empty(t, principal.SubjectName)
	require.Equal(t, 0, names.calls)
}

// The three Channels permissions reach the handler independently, from the
// grants the authorizer loaded for this request and from nothing on the token.
func TestChannelsAuthenticatorCarriesEachPermissionIndependently(t *testing.T) {
	t.Parallel()
	config := channelsTestConfig(t)
	grant := func(scope authz.Scope) authz.Grant { return authz.NewGrant(scope, channelsTestProjectID.String()) }

	for _, tc := range []struct {
		name                    string
		grants                  []authz.Grant
		connect, admin, readAll bool
	}{
		{name: "no grants"},
		{name: "mcp access grants nothing", grants: []authz.Grant{grant(authz.ScopeMCPConnect), grant(authz.ScopeOrgAdmin)}},
		{name: "connect only", grants: []authz.Grant{grant(authz.ScopeChannelsConnect)}, connect: true},
		{name: "admin only", grants: []authz.Grant{grant(authz.ScopeChannelsAdmin)}, admin: true},
		{name: "read all only", grants: []authz.Grant{grant(authz.ScopeChannelsReadAll)}, readAll: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			authenticate := newChannelsAuthenticator(config, nil, &channelsTestSessions{}, channelsAdmittedPreparer{grants: tc.grants}, nil)
			request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/api", nil)
			request.Header.Set("Origin", "https://app.example.test")
			principal, err := authenticate(request)
			require.NoError(t, err)
			require.Equal(t, tc.connect, principal.CanConnect)
			require.Equal(t, tc.admin, principal.CanAdmin)
			require.Equal(t, tc.readAll, principal.CanReadAll)
		})
	}
}

// channelsAdmittedPreparer stands in for channelsAdmission: it admits the
// principal and returns the context that Channels admission builds — an authenticated
// actor with no browser session, the acting surface marked so RBAC enforces,
// and freshly loaded grants.
type channelsAdmittedPreparer struct {
	grants []authz.Grant
}

func (p channelsAdmittedPreparer) PrepareContext(ctx context.Context, principal authn.Identity) (context.Context, error) {
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		UserID:               principal.Principal.ID,
		ActiveOrganizationID: principal.OrganizationID,
	}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.Principal.ID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfaceChannels)
	return authz.GrantsToContext(ctx, p.grants), nil
}

func channelsTestConfig(t *testing.T) channelsConfig {
	t.Helper()
	serverURL, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil)
	features := new(feature.InMemory)
	features.SetFlag(feature.FlagChannels, feature.AnyDistinctID, true)
	return channelsConfig{Features: features, ServerURL: serverURL, DashboardURL: dashboardURL, Authz: engine,
		resolveTarget: func(_ context.Context, origin, resource string) (authn.Target, error) {
			return authn.Target{OrganizationID: "org_example", ProjectID: channelsTestProjectID, OrganizationSlug: "oauth-test", ProjectSlug: "default", Resource: resource}, nil
		},
	}
}

func TestChannelsProtocolVersionSupported(t *testing.T) {
	t.Parallel()
	require.True(t, channelsProtocolVersionSupported("2026-07-28"))
	require.True(t, channelsProtocolVersionSupported("2025-03-26"))
	require.False(t, channelsProtocolVersionSupported("2099-01-01"))
}

// Every 401 carries the Channels resource metadata, or a client holding an
// expired or wrong-audience token cannot discover it must authorize again for
// this audience. No other status carries a challenge.
func TestChannelsAuthChallengeAccompaniesEveryUnauthorized(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusOK, http.StatusForbidden} {
		recorder := httptest.NewRecorder()
		writer := &channelsAuthChallenge{ResponseWriter: recorder, metadata: "https://api.example.test/.well-known/oauth-protected-resource/channels/oauth-test/default/mcp"}
		writer.WriteHeader(status)
		if status == http.StatusUnauthorized {
			require.Equal(t, `Bearer resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/channels/oauth-test/default/mcp"`, recorder.Header().Get("WWW-Authenticate"))
			continue
		}
		require.Empty(t, recorder.Header().Get("WWW-Authenticate"))
	}
}

// Subscription streams set a write deadline through http.ResponseController,
// which reaches the real writer only through Unwrap. The call discards its
// error, so a wrapper that hid the writer would drop the deadline silently.
func TestChannelsAuthChallengeKeepsStreamControlsReachable(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writer := &channelsAuthChallenge{ResponseWriter: recorder, metadata: "https://api.example.test/metadata"}
	require.Same(t, recorder, writer.Unwrap())
	writer.Flush()
	require.True(t, recorder.Flushed)
}

func TestChannelsShutdownDrainsStreams(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closer := &channelsTestCloser{drained: make(chan struct{})}
	closeChannelsOnShutdown(ctx, nil, closer)
	cancel()
	select {
	case <-closer.drained:
	case <-time.After(time.Second):
		t.Fatal("channels subscription streams were not drained on shutdown")
	}
}

type channelsTestCloser struct{ drained chan struct{} }

func (c *channelsTestCloser) Shutdown(ctx context.Context) error {
	close(c.drained)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("drain deadline: %w", err)
	}
	return nil
}

// A client software identity is not implicitly a control-plane agent identity.
// Native machine credential admission remains an explicit future contract.
type channelsTypedTokens struct{ identity authn.Identity }

func (t channelsTypedTokens) Authenticate(context.Context, string) (authn.Identity, error) {
	return t.identity, nil
}

func TestChannelsAuthenticatorRejectsNonUserActors(t *testing.T) {
	t.Parallel()
	for _, kind := range []urn.PrincipalType{urn.PrincipalTypeAgent, urn.PrincipalTypeWorkload, urn.PrincipalTypeAPIKey} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			preparer := &channelsTestPreparer{}
			token := channelsTypedTokens{identity: authn.Identity{OrganizationID: "org_example", ProjectID: channelsTestProjectID, Principal: urn.NewPrincipal(kind, "actor_example")}}
			authenticate := newChannelsAuthenticator(channelsTestConfig(t), token, nil, preparer, nil)
			request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/mcp", nil)
			request.Header.Set("Authorization", "Bearer example")
			_, err := authenticate(request)
			require.ErrorContains(t, err, "invalid channels principal")
			require.Zero(t, preparer.calls)
		})
	}
}

func TestChannelsAuthenticatorMissingCredentialValidatorFailsClosed(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/mcp", nil)
	request.Header.Set("Authorization", "Bearer example")
	authenticate := newChannelsAuthenticator(channelsTestConfig(t), nil, nil, &channelsTestPreparer{}, nil)
	_, err := authenticate(request)
	require.ErrorContains(t, err, "credential authentication unavailable")
}

func TestChannelsOAuthRoutesConfinedToPlatformIngress(t *testing.T) {
	t.Parallel()
	for _, surface := range []requestorigin.Surface{requestorigin.SurfacePlatform, requestorigin.SurfaceCustomDomain, requestorigin.SurfacePrivateNetwork} {
		t.Run(string(surface), func(t *testing.T) {
			t.Parallel()
			mux := goahttp.NewMuxer()
			channelsOAuthMux{mux: mux}.Handle(http.MethodGet, "/channels/oauth/authorize", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			ctx := requestorigin.WithContext(t.Context(), requestorigin.Origin{Surface: surface})
			request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/channels/oauth/authorize", nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if surface == requestorigin.SurfacePlatform {
				require.Equal(t, http.StatusNoContent, response.Code)
			} else {
				require.Equal(t, http.StatusNotFound, response.Code)
			}
		})
	}
}

func TestConfigureChannelsRequiresConfiguration(t *testing.T) {
	t.Parallel()
	require.ErrorContains(t, configureChannels(t.Context(), channelsConfig{}), "incomplete channels configuration")
}

// Test-only Platform imports prove credential-store separation against the real
// existing validator, not merely a token signed for another audience.
func TestChannelsOAuthLifecycleDoesNotMutatePlatformCredentials(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "channels_platform_independence")
	require.NoError(t, err)
	const orgID = "org_example"
	const userID = "user_example"
	const origin = "https://api.example.test"
	const redirect = "https://client.example.test/callback"
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "OAuth test organization", Slug: "oauth-test"})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Default", Slug: "default", OrganizationID: orgID})
	require.NoError(t, err)
	ctx = authn.WithTarget(ctx, authn.Target{OrganizationID: orgID, ProjectID: project.ID, OrganizationSlug: "oauth-test", ProjectSlug: "default", Resource: origin + "/channels/oauth-test/default/mcp"})
	base, err := url.Parse(origin)
	require.NoError(t, err)
	signer := sessiontokens.NewSigner("channels-platform-isolation-test-key")
	oauth, err := authn.New(authn.Config{BaseURL: base, DB: db, Signer: signer,
		AuthenticateBrowser: func(r *http.Request) (context.Context, error) {
			return contextvalues.WithValidatedGramSession(r.Context(), &contextvalues.AuthContext{UserID: userID, ActiveOrganizationID: orgID}, false), nil
		},
		Admit: func(ctx context.Context, _ authn.Identity) (context.Context, error) { return ctx, nil },
	})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	oauth.Attach(mux)
	request := func(method, path, contentType, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, method, origin+path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if method == http.MethodPost && path == "/channels/oauth/authorize" {
			req.Header.Set("Origin", origin)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		return response
	}
	registration := request(http.MethodPost, "/channels/oauth/register", "application/json", `{"client_name":"Compatibility test client","redirect_uris":["https://client.example.test/callback"],"token_endpoint_auth_method":"none"}`)
	require.Equal(t, http.StatusCreated, registration.Code, registration.Body.String())
	var client struct {
		ID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(registration.Body.Bytes(), &client))
	require.NotEmpty(t, client.ID)
	// Use exactly the same registered client ID, account, organization, and signer.
	platformStore := platformmcp.NewPostgresOAuthStore(db)
	require.NoError(t, platformStore.RegisterClient(ctx, platformoauth.Client{ID: client.ID, Name: "Compatibility test client", RedirectURIs: []string{redirect}}))
	now := time.Now().UTC()
	connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: urn.NewUserSubject(userID).String(), OrganizationID: orgID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime)}
	require.NoError(t, platformStore.RegisterConnection(ctx, connection))
	cipher, err := encryption.NewWithBytes([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	codec, err := platformmcp.NewCredentialCodec(cipher)
	require.NoError(t, err)
	jti, err := codec.Issue("access_jti", orgID, origin+"/platform-mcp")
	require.NoError(t, err)
	expiry := now.Add(time.Hour)
	platformToken, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject(userID), Audience: origin + "/platform-mcp", Issuer: origin + "/platform-mcp", ExpiresAt: &expiry, ClientID: client.ID, JTI: jti})
	require.NoError(t, err)
	require.NoError(t, platformStore.CreateSession(ctx, platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: jti, RefreshHash: "platform-refresh-test-hash", ExpiresAt: expiry, RefreshExpiresAt: expiry}))
	platformAuth, err := platformmcp.NewJWTAuthenticator(signer, db, cipher, base)
	require.NoError(t, err)
	assertPlatformActive := func() {
		t.Helper()
		principal, err := platformAuth.Authenticate(ctx, platformToken)
		require.NoError(t, err)
		require.Equal(t, client.ID, principal.ClientID)
		require.Equal(t, userID, principal.UserID)
		_, err = oauth.Authenticate(ctx, platformToken)
		require.Error(t, err, "Platform credential must not authenticate to Channels")
	}
	assertPlatformActive()
	verifier := strings.Repeat("a", 43)
	challenge := sha256.Sum256([]byte(verifier))
	values := url.Values{"client_id": {client.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "resource": {origin + "/channels/oauth-test/default/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}}
	consent := request(http.MethodGet, "/channels/oauth/authorize?"+values.Encode(), "", " ")
	require.Equal(t, http.StatusOK, consent.Code, consent.Body.String())
	hidden := func(name string) string {
		t.Helper()
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(consent.Body.String())
		require.Len(t, match, 2, consent.Body.String())
		return match[1]
	}
	values = url.Values{"resource": {hidden("resource")}, "challenge": {hidden("challenge")}, "csrf": {hidden("csrf")}, "decision": {"allow"}}
	authorized := request(http.MethodPost, "/channels/oauth/authorize", "application/x-www-form-urlencoded", values.Encode(), consent.Result().Cookies()...)
	require.Equal(t, http.StatusSeeOther, authorized.Code, authorized.Body.String())
	callback, err := url.Parse(authorized.Header().Get("Location"))
	require.NoError(t, err)
	require.NotEmpty(t, callback.Query().Get("code"))
	assertPlatformActive()
	type tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	exchange := func(fields url.Values) tokens {
		t.Helper()
		response := request(http.MethodPost, "/channels/oauth/token", "application/x-www-form-urlencoded", fields.Encode())
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result tokens
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.NotEmpty(t, result.Access)
		require.NotEmpty(t, result.Refresh)
		_, err = oauth.Authenticate(ctx, result.Access)
		require.NoError(t, err)
		_, err = platformAuth.Authenticate(ctx, result.Access)
		require.Error(t, err, "Channels credential must not authenticate to Platform")
		assertPlatformActive()
		return result
	}
	issued := exchange(url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {callback.Query().Get("code")}, "redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {origin + "/channels/oauth-test/default/mcp"}})
	rotated := exchange(url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {issued.Refresh}, "resource": {origin + "/channels/oauth-test/default/mcp"}})
	// An open subscription must consult the same live Channels credential store,
	// not keep the admission-time identity after its grant is revoked.
	_, err = channelsrepo.New(db).EnsureChannelsSettings(ctx, channelsrepo.EnsureChannelsSettingsParams{OrganizationID: orgID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = channelsrepo.New(db).SetChannelsEnabled(ctx, channelsrepo.SetChannelsEnabledParams{OrganizationID: orgID, ProjectID: project.ID, Enabled: true})
	require.NoError(t, err)
	limits := channels.DefaultLimits()
	limits.ReconcileInterval = 50 * time.Millisecond
	config := channelsTestConfig(t)
	config.DB = db
	config.resolveTarget = nil
	handler := channels.New(db, newChannelsAuthenticator(config, oauth, nil, channelsAdmittedPreparer{grants: []authz.Grant{authz.NewGrant(authz.ScopeChannelsConnect, project.ID.String())}}, nil), channels.WithLimits(limits))
	streamServer := httptest.NewServer(handler)
	t.Cleanup(streamServer.Close)
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		require.NoError(t, handler.Shutdown(shutdownCtx))
	})
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodPost, streamServer.URL+"/channels/oauth-test/default/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"revocation","method":"subscriptions/listen","params":{"notifications":{"resourceSubscriptions":["channels://presence"]}}}`))
	require.NoError(t, err)
	streamRequest.Header.Set("Authorization", "Bearer "+rotated.Access)
	streamResponse, err := streamServer.Client().Do(streamRequest)
	require.NoError(t, err)
	defer func() { require.NoError(t, streamResponse.Body.Close()) }()
	require.Equal(t, http.StatusOK, streamResponse.StatusCode)
	scanner := bufio.NewScanner(streamResponse.Body)
	nextEvent := func() map[string]any {
		t.Helper()
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), "data: ") {
				continue
			}
			var event map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event))
			return event
		}
		require.FailNow(t, "subscription ended without the expected event", "%v", scanner.Err())
		return nil
	}
	require.Equal(t, "notifications/subscriptions/acknowledged", nextEvent()["method"])

	revoked := request(http.MethodPost, "/channels/oauth/revoke", "application/x-www-form-urlencoded", url.Values{"client_id": {client.ID}, "token": {rotated.Refresh}, "resource": {origin + "/channels/oauth-test/default/mcp"}}.Encode())
	require.Equal(t, http.StatusOK, revoked.Code, revoked.Body.String())
	completion := nextEvent()
	result, ok := completion["result"].(map[string]any)
	require.True(t, ok, "unexpected completion: %v", completion)
	require.Equal(t, "complete", result["resultType"])

	_, err = oauth.Authenticate(ctx, rotated.Access)
	require.Error(t, err)
	assertPlatformActive()
}

func TestChannelsAdmissionRebuildsLiveMembershipAndGrants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "channels_live_admission")
	require.NoError(t, err)
	orgID, userID := "org_"+uuid.NewString(), "user_"+uuid.NewString()
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Channels test", Slug: "channels-" + uuid.NewString()})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: userID, Email: userID + "@example.test", DisplayName: "Test user"})
	require.NoError(t, err)
	_, err = organizationsrepo.New(db).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText(userID)})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Target project", Slug: "target", OrganizationID: orgID})
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeUser, userID)
	selectors, err := authz.NewSelector(authz.ScopeChannelsConnect, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	grant, err := accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: orgID, PrincipalUrn: principal, Scope: string(authz.ScopeChannelsConnect), Selectors: selectors})
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	features := new(feature.InMemory)
	admission := channelsAdmission{db: db, engine: engine, features: features}
	identity := authn.Identity{OrganizationID: orgID, ProjectID: project.ID, Principal: principal}
	_, err = admission.PrepareContext(ctx, identity)
	require.ErrorContains(t, err, "not enabled for this organization")
	features.SetFlag(feature.FlagChannels, orgID, true)
	_, err = admission.PrepareContext(ctx, identity)
	require.ErrorContains(t, err, "project access required", "Channels permission alone must not grant project access")
	readSelectors, err := authz.NewSelector(authz.ScopeProjectRead, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	readGrant, err := accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: orgID, PrincipalUrn: principal, Scope: string(authz.ScopeProjectRead), Selectors: readSelectors})
	require.NoError(t, err)
	inherited := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeChannelsReadAll, project.ID.String())})
	prepared, err := admission.PrepareContext(inherited, identity)
	require.NoError(t, err)
	allowed, err := engine.Evaluate(prepared, authz.Check{Scope: authz.ScopeChannelsConnect, ResourceID: project.ID.String(), ResourceKind: "project"})
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = engine.Evaluate(prepared, authz.Check{Scope: authz.ScopeChannelsReadAll, ResourceID: project.ID.String(), ResourceKind: "project"})
	require.NoError(t, err)
	require.False(t, allowed, "inherited grants must not cross the authentication boundary")
	_, err = accessrepo.New(db).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: grant.ID, OrganizationID: orgID})
	require.NoError(t, err)
	// SSE reconciliation must rebuild even with a previously admitted context.
	prepared, err = admission.PrepareContext(prepared, identity)
	require.NoError(t, err)
	allowed, err = engine.Evaluate(prepared, authz.Check{Scope: authz.ScopeChannelsConnect, ResourceID: project.ID.String(), ResourceKind: "project"})
	require.NoError(t, err)
	require.False(t, allowed)
	features.SetFlag(feature.FlagChannels, orgID, false)
	_, err = admission.PrepareContext(prepared, identity)
	require.ErrorContains(t, err, "not enabled for this organization")
	features.SetFlag(feature.FlagChannels, orgID, true)
	_, err = accessrepo.New(db).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{ID: readGrant.ID, OrganizationID: orgID})
	require.NoError(t, err)
	_, err = admission.PrepareContext(prepared, identity)
	require.ErrorContains(t, err, "project access required", "reconciliation must not retain revoked project read access")
	err = organizationsrepo.New(db).DeleteOrganizationUserRelationship(ctx, organizationsrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText(userID)})
	require.NoError(t, err)
	_, err = admission.PrepareContext(prepared, identity)
	require.ErrorContains(t, err, "membership required")
}

func TestChannelsOAuthDefersToEffectiveAccessExceptions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "channels_oauth_effective_access")
	require.NoError(t, err)
	const orgID = "org_example"
	const userID = "user_example"
	const origin = "https://api.example.test"
	const redirect = "https://client.example.test/callback"
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: orgID, Name: "Channels access test", Slug: "channels-access-test"})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Default", Slug: "default", OrganizationID: orgID})
	require.NoError(t, err)
	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{ID: userID, Email: "member@example.test", DisplayName: "Test member"})
	require.NoError(t, err)
	_, err = organizationsrepo.New(db).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText(userID)})
	require.NoError(t, err)
	_, err = channelsrepo.New(db).EnsureChannelsSettings(ctx, channelsrepo.EnsureChannelsSettingsParams{OrganizationID: orgID, ProjectID: project.ID})
	require.NoError(t, err)
	_, err = channelsrepo.New(db).SetChannelsEnabled(ctx, channelsrepo.SetChannelsEnabledParams{OrganizationID: orgID, ProjectID: project.ID, Enabled: true})
	require.NoError(t, err)
	setException := func(allow bool) {
		t.Helper()
		_, err := channelsrepo.New(db).UpsertChannelsAccessException(ctx, channelsrepo.UpsertChannelsAccessExceptionParams{OrganizationID: orgID, ProjectID: project.ID, SubjectID: userID, CanConnect: allow})
		require.NoError(t, err)
	}
	readSelectors, err := authz.NewSelector(authz.ScopeProjectRead, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, userID), Scope: string(authz.ScopeProjectRead), Selectors: readSelectors})
	require.NoError(t, err)
	setException(true)
	config := channelsTestConfig(t)
	config.DB = db
	config.resolveTarget = nil
	config.Mux = goahttp.NewMuxer()
	config.Logger = testenv.NewLogger(t)
	config.Authz = authz.NewEngine(config.Logger, db, func(context.Context, string) (bool, error) { return false, nil }, nil)
	config.Sessions = &channelsTestSessions{}
	config.JWTSigningKey = "channels-effective-access-test-key"
	admission := channelsAdmission{db: db, engine: config.Authz, features: config.Features}
	identity := authn.Identity{OrganizationID: orgID, ProjectID: project.ID, Principal: urn.NewPrincipal(urn.PrincipalTypeUser, userID)}
	nativeConnect := func() bool {
		t.Helper()
		prepared, err := admission.PrepareContext(ctx, identity)
		require.NoError(t, err)
		allowed, err := config.Authz.Evaluate(prepared, authz.Check{Scope: authz.ScopeChannelsConnect, ResourceID: project.ID.String(), ResourceKind: "project"})
		require.NoError(t, err)
		return allowed
	}
	require.False(t, nativeConnect(), "fixture must not have a native connect grant")
	serviceCtx, cancelService := context.WithCancel(ctx)
	t.Cleanup(cancelService)
	require.NoError(t, configureChannels(serviceCtx, config))
	server := httptest.NewServer(config.Mux)
	t.Cleanup(func() { cancelService(); server.Close() })
	request := func(method, path, contentType, body, bearer string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, method, origin+path, strings.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if path == "/channels/oauth/authorize" && method == http.MethodPost {
			req.Header.Set("Origin", origin)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		config.Mux.ServeHTTP(response, req)
		return response
	}
	registered := request(http.MethodPost, "/channels/oauth/register", "application/json", `{"client_name":"Channels access test client","redirect_uris":["https://client.example.test/callback"],"token_endpoint_auth_method":"none"}`, "")
	require.Equal(t, http.StatusCreated, registered.Code, registered.Body.String())
	var client struct {
		ID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(registered.Body.Bytes(), &client))
	verifier := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(verifier))
	query := url.Values{"client_id": {client.ID}, "redirect_uri": {redirect}, "response_type": {"code"}, "resource": {origin + "/channels/channels-access-test/default/mcp"}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}}
	consent := request(http.MethodGet, "/channels/oauth/authorize?"+query.Encode(), "", "", "")
	require.Equal(t, http.StatusOK, consent.Code, consent.Body.String())
	hidden := func(name string) string {
		t.Helper()
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(consent.Body.String())
		require.Len(t, match, 2, consent.Body.String())
		return match[1]
	}
	approved := request(http.MethodPost, "/channels/oauth/authorize", "application/x-www-form-urlencoded", url.Values{"resource": {hidden("resource")}, "challenge": {hidden("challenge")}, "csrf": {hidden("csrf")}, "decision": {"allow"}}.Encode(), "", consent.Result().Cookies()...)
	require.Equal(t, http.StatusSeeOther, approved.Code, approved.Body.String())
	callback, err := url.Parse(approved.Header().Get("Location"))
	require.NoError(t, err)
	tokenResponse := request(http.MethodPost, "/channels/oauth/token", "application/x-www-form-urlencoded", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {callback.Query().Get("code")}, "redirect_uri": {redirect}, "code_verifier": {verifier}, "resource": {origin + "/channels/channels-access-test/default/mcp"}}.Encode(), "")
	require.Equal(t, http.StatusOK, tokenResponse.Code, tokenResponse.Body.String())
	var issued struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(tokenResponse.Body.Bytes(), &issued))
	require.NotEmpty(t, issued.Access)
	callConnect := func(label string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "channels_connect", "arguments": map[string]any{"label": label}}})
		require.NoError(t, err)
		return request(http.MethodPost, "/channels/channels-access-test/default/mcp", "application/json", string(body), issued.Access)
	}
	connected := callConnect("Exception-granted instance")
	require.Equal(t, http.StatusOK, connected.Code, connected.Body.String())
	var connectedBody map[string]any
	require.NoError(t, json.Unmarshal(connected.Body.Bytes(), &connectedBody))
	require.Nil(t, connectedBody["error"], connected.Body.String())
	require.NotNil(t, connectedBody["result"])
	// A native allow does not override a later explicit deny.
	selectors, err := authz.NewSelector(authz.ScopeChannelsConnect, project.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: orgID, PrincipalUrn: identity.Principal, Scope: string(authz.ScopeChannelsConnect), Selectors: selectors})
	require.NoError(t, err)
	require.True(t, nativeConnect())
	streamCtx, cancelStream := context.WithTimeout(ctx, 25*time.Second)
	defer cancelStream()
	streamRequest, err := http.NewRequestWithContext(streamCtx, http.MethodPost, server.URL+"/channels/channels-access-test/default/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":"exception-revocation","method":"subscriptions/listen","params":{"notifications":{"resourceSubscriptions":["channels://presence"]}}}`))
	require.NoError(t, err)
	streamRequest.Header.Set("Authorization", "Bearer "+issued.Access)
	stream, err := server.Client().Do(streamRequest)
	require.NoError(t, err)
	defer func() { require.NoError(t, stream.Body.Close()) }()
	require.Equal(t, http.StatusOK, stream.StatusCode)
	scanner := bufio.NewScanner(stream.Body)
	nextEvent := func() map[string]any {
		t.Helper()
		for scanner.Scan() {
			if !strings.HasPrefix(scanner.Text(), "data: ") {
				continue
			}
			var event map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &event))
			return event
		}
		require.FailNow(t, "stream closed without expected event", "%v", scanner.Err())
		return nil
	}
	require.Equal(t, "notifications/subscriptions/acknowledged", nextEvent()["method"])
	setException(false)
	denied := callConnect("Denied instance")
	var denial map[string]any
	require.NoError(t, json.Unmarshal(denied.Body.Bytes(), &denial))
	require.NotNil(t, denial["error"], denied.Body.String())
	require.True(t, nativeConnect(), "the exception must override a still-present native grant")
	// Authentication and refresh remain valid; operation permissions are not
	// embedded in or cached by the credential lifecycle.
	refreshed := request(http.MethodPost, "/channels/oauth/token", "application/x-www-form-urlencoded", url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ID}, "refresh_token": {issued.Refresh}, "resource": {origin + "/channels/channels-access-test/default/mcp"}}.Encode(), "")
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	completion := nextEvent()
	result, ok := completion["result"].(map[string]any)
	require.True(t, ok, "unexpected completion: %v", completion)
	require.Equal(t, "complete", result["resultType"])
}

func TestChannelsAuthenticatorRejectsInvalidDashboardSessions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		session channelsTestSessions
	}{
		{name: "unvalidated session", session: channelsTestSessions{unvalidated: true}},
		{name: "mismatched actor", session: channelsTestSessions{actorID: "user_other"}},
		{name: "support session", session: channelsTestSessions{support: true}},
		{name: "legacy impersonation", session: channelsTestSessions{impersonated: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			session := tc.session
			preparer := &channelsTestPreparer{}
			authenticate := newChannelsAuthenticator(channelsTestConfig(t), nil, &session, preparer, nil)
			request := httptest.NewRequest(http.MethodGet, "/channels/oauth-test/default/api", nil)
			_, err := authenticate(request)
			require.ErrorContains(t, err, "invalid channels session")
			require.Zero(t, preparer.calls)
		})
	}
}

func TestChannelsAuthenticatorRejectsBearerTargetMismatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		organizationID string
		projectID      uuid.UUID
	}{
		{name: "other organization", organizationID: "org_other", projectID: channelsTestProjectID},
		{name: "other project", organizationID: "org_example", projectID: uuid.New()},
		{name: "missing project", organizationID: "org_example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := channelsTypedTokens{identity: authn.Identity{OrganizationID: tc.organizationID, ProjectID: tc.projectID, Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "user_example")}}
			preparer := &channelsTestPreparer{}
			authenticate := newChannelsAuthenticator(channelsTestConfig(t), token, nil, preparer, nil)
			request := httptest.NewRequest(http.MethodPost, "/channels/oauth-test/default/mcp", nil)
			request.Header.Set("Authorization", "Bearer example")
			_, err := authenticate(request)
			require.ErrorContains(t, err, "target mismatch")
			require.Zero(t, preparer.calls)
		})
	}
}
