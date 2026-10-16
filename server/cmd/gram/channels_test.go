package gram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type channelsTestTokens struct{ calls int }

func (a *channelsTestTokens) AuthenticateChannels(context.Context, string) (platformmcp.Principal, error) {
	a.calls++
	return platformmcp.Principal{UserID: "user_example", OrganizationID: "org_example", Surface: platformmcp.SurfaceChannels}, nil
}

type channelsTestSessions struct {
	calls int
	email string
}

func (a *channelsTestSessions) AuthenticateWithCookie(ctx context.Context) (context.Context, error) {
	a.calls++
	auth := &contextvalues.AuthContext{UserID: "user_example", ActiveOrganizationID: "org_example"}
	if a.email != "" {
		auth.Email = conv.PtrEmpty(a.email)
	}
	return contextvalues.WithAuthenticatedActor(ctx, auth, urn.NewPrincipal(urn.PrincipalTypeUser, "user_example")), nil
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
	principal platformmcp.Principal
}

func (a *channelsTestPreparer) PrepareExternalContext(ctx context.Context, principal platformmcp.Principal) (context.Context, error) {
	a.calls++
	a.principal = principal
	return ctx, errors.New("revoked")
}

func TestChannelsAuthenticatorRevalidatesBearerEveryCall(t *testing.T) {
	t.Parallel()
	tokens, preparer := &channelsTestTokens{}, &channelsTestPreparer{}
	authenticate := newChannelsAuthenticator(platformMCPConfig{}, tokens, nil, preparer, nil)
	request := httptest.NewRequest(http.MethodPost, "/channels/mcp", nil)
	request.Header.Set("Authorization", "bEaReR example")
	for range 2 {
		_, err := authenticate(request)
		require.ErrorContains(t, err, "revoked")
	}
	require.Equal(t, 2, tokens.calls)
	require.Equal(t, 2, preparer.calls)
}

func TestChannelsAuthenticatorBrowserSessionBoundary(t *testing.T) {
	t.Parallel()
	serverURL, _ := url.Parse("https://api.example.test")
	dashboardURL, _ := url.Parse("https://app.example.test")
	for _, tc := range []struct {
		name, path, origin string
		sessionCalls       int
	}{
		{name: "mcp rejects cookies", path: "/channels/mcp", origin: "https://app.example.test"},
		{name: "api rejects foreign origin", path: "/channels/api", origin: "https://other.example.test"},
		{name: "api rejects missing origin", path: "/channels/api"},
		{name: "api accepts dashboard origin", path: "/channels/api", origin: "https://app.example.test", sessionCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sessions, preparer := &channelsTestSessions{}, &channelsTestPreparer{}
			authenticate := newChannelsAuthenticator(platformMCPConfig{ServerURL: serverURL, DashboardURL: dashboardURL}, nil, sessions, preparer, nil)
			request := httptest.NewRequest(http.MethodPost, tc.path, nil)
			request.Header.Set("Origin", tc.origin)
			_, err := authenticate(request)
			require.Error(t, err)
			require.Equal(t, tc.sessionCalls, sessions.calls)
			require.Equal(t, tc.sessionCalls, preparer.calls)
			if tc.sessionCalls != 0 {
				require.Equal(t, "org_example", preparer.principal.OrganizationID)
				require.Equal(t, "user_example", preparer.principal.UserID)
				require.Equal(t, platformmcp.SurfaceChannels, preparer.principal.Surface)
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
			request := httptest.NewRequest(http.MethodPost, "/channels/api", nil)
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
	principal, err := authenticate(httptest.NewRequest(http.MethodGet, "/channels/api", nil))
	require.NoError(t, err)
	require.Empty(t, principal.SubjectName)
	require.Equal(t, 0, names.calls)
}

// The three Channels permissions reach the handler independently, from the
// grants the authorizer loaded for this request and from nothing on the token.
func TestChannelsAuthenticatorCarriesEachPermissionIndependently(t *testing.T) {
	t.Parallel()
	config := channelsTestConfig(t)
	grant := func(scope authz.Scope) authz.Grant { return authz.NewGrant(scope, "org_example") }

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
			request := httptest.NewRequest(http.MethodPost, "/channels/api", nil)
			request.Header.Set("Origin", "https://app.example.test")
			principal, err := authenticate(request)
			require.NoError(t, err)
			require.Equal(t, tc.connect, principal.CanConnect)
			require.Equal(t, tc.admin, principal.CanAdmin)
			require.Equal(t, tc.readAll, principal.CanReadAll)
		})
	}
}

// channelsAdmittedPreparer stands in for LiveOrgAdminAuthorizer: it admits the
// principal and returns the context that authorizer builds — an authenticated
// actor with no browser session, the acting surface marked so RBAC enforces,
// and freshly loaded grants.
type channelsAdmittedPreparer struct {
	grants []authz.Grant
}

func (p channelsAdmittedPreparer) PrepareExternalContext(ctx context.Context, principal platformmcp.Principal) (context.Context, error) {
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		UserID:               principal.UserID,
		ActiveOrganizationID: principal.OrganizationID,
	}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, string(platformmcp.SurfaceChannels))
	return authz.GrantsToContext(ctx, p.grants), nil
}

func channelsTestConfig(t *testing.T) platformMCPConfig {
	t.Helper()
	serverURL, err := url.Parse("https://api.example.test")
	require.NoError(t, err)
	dashboardURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil)
	return platformMCPConfig{ServerURL: serverURL, DashboardURL: dashboardURL, Authz: engine}
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
		writer := &channelsAuthChallenge{ResponseWriter: recorder, metadata: "https://api.example.test/.well-known/oauth-protected-resource/channels/mcp"}
		writer.WriteHeader(status)
		if status == http.StatusUnauthorized {
			require.Equal(t, `Bearer resource_metadata="https://api.example.test/.well-known/oauth-protected-resource/channels/mcp"`, recorder.Header().Get("WWW-Authenticate"))
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

// A deployment without the Channels rollout gate registers no Channels route,
// so the endpoint and the dashboard API both answer the mux's own 404. The
// denial must not name the feature or its permissions: probing an unreleased
// surface should learn nothing from the response.
func TestChannelsRolloutGateWithholdsTheEndpoints(t *testing.T) {
	t.Parallel()
	mux := goahttp.NewMuxer()
	attachChannels(t.Context(), platformMCPConfig{Mux: mux, Logger: testenv.NewLogger(t)}, nil, nil)

	for _, path := range []string{"/channels/mcp", "/channels/api"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
			require.Equal(t, http.StatusNotFound, recorder.Code, "%s %s", method, path)
			require.NotContains(t, recorder.Body.String(), "channels:")
			require.NotContains(t, recorder.Body.String(), "Channels")
		}
	}
}
