package remotemcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func loadProtectedResource(t *testing.T, ctx context.Context, ti *testInstance, resourceURL string) repo.RemoteProtectedResource {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{
		ProjectID:          *authCtx.ProjectID,
		ResourceIdentifier: resourceURL,
	})
	require.NoError(t, err)
	return row
}

// launchRawResourceMetadata serves body verbatim at the origin-style
// well-known path, with "{{origin}}" replaced by the server's own URL.
func launchRawResourceMetadata(t *testing.T, body string) *httptest.Server {
	t.Helper()
	var origin string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.ReplaceAll(body, "{{origin}}", origin)))
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL
	return upstream
}

// The re-probe records the document on the resource row: display members,
// the verbatim document, where it was read from, and a NULL scopes_supported
// when the document omits the member.
func TestUpdateServer_ReprobeRecordsProtectedResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream, _ := launchResourceMetadata(t, "Example MCP", "https://docs.example.test/mcp", "https://example.test/tos")

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	seedAttachedResource(t, ctx, ti, server, "https://old.example.test")
	updateServerURL(t, ctx, ti, server, upstream.URL)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
	require.Equal(t, upstream.URL+wellknown.OAuthProtectedResourcePath, row.MetadataUrl.String)
	require.Equal(t, []string{"https://auth.example.test"}, row.AuthorizationServers)
	require.Nil(t, row.ScopesSupported, "an omitted scopes_supported is NULL")
	require.Nil(t, row.BearerMethodsSupported)
	require.Equal(t, "Example MCP", row.ResourceName.String)
	require.Equal(t, "https://docs.example.test/mcp", row.ResourceDocumentation.String)
	require.False(t, row.ResourcePolicyUri.Valid)
	require.Equal(t, "https://example.test/tos", row.ResourceTosUri.String)
	require.True(t, row.MetadataFetchedAt.Valid)
	require.False(t, row.MetadataLastError.Valid)
	require.False(t, row.MetadataLastErrorAt.Valid)
	require.Nil(t, row.ChallengeScopes)

	var stored map[string]any
	require.NoError(t, json.Unmarshal(row.Metadata, &stored))
	require.Equal(t, upstream.URL, stored["resource"])
	require.Equal(t, "Example MCP", stored["resource_name"])
}

// A document advertising an empty scopes_supported records an empty array,
// not NULL.
func TestUpdateServer_ReprobeRecordsEmptyScopesSupported(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream := launchRawResourceMetadata(t, `{"resource":"{{origin}}","authorization_servers":["https://auth.example.test"],"scopes_supported":[],"bearer_methods_supported":["header"],"dpop_bound_access_tokens_required":true,"dpop_signing_alg_values_supported":["ES256"]}`)

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	seedAttachedResource(t, ctx, ti, server, "https://old.example.test")
	updateServerURL(t, ctx, ti, server, upstream.URL)

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.NotNil(t, row.ScopesSupported, "an advertised empty array is not NULL")
	require.Empty(t, row.ScopesSupported)
	require.Equal(t, []string{"header"}, row.BearerMethodsSupported)
	require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, row.DpopBoundAccessTokensRequired)
	require.Equal(t, []string{"ES256"}, row.DpopSigningAlgValuesSupported)
	require.False(t, row.TlsClientCertificateBoundAccessTokens.Valid, "an omitted member is NULL")
}

// An omitted authorization_servers is NULL, not an empty array.
func TestUpdateServer_ReprobeRecordsOmittedAuthorizationServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))
	upstream := launchRawResourceMetadata(t, `{"resource":"{{origin}}"}`)

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://old.example.test")
	seedAttachedResource(t, ctx, ti, server, "https://old.example.test")
	updateServerURL(t, ctx, ti, server, upstream.URL)

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Nil(t, row.AuthorizationServers)
}

// A failed probe leaves a row carrying the public-safe reason and nothing
// advertised; a later successful re-probe fills the same row and clears it.
func TestUpdateServer_FailedReprobeRecordsErrorThenRecoversInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	var origin string
	var available atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available.Load() || r.URL.Path != wellknown.OAuthProtectedResourcePath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resource":              origin,
			"authorization_servers": []string{"https://auth.example.test"},
			"scopes_supported":      []string{"read"},
			"resource_name":         "Resource B",
		})
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL

	server := seedRemoteMcpServerWithURL(t, ctx, ti, "https://a.example.test/mcp")
	seedAttachedResource(t, ctx, ti, server, "https://a.example.test/mcp")
	updateServerURL(t, ctx, ti, server, origin)

	failed := loadProtectedResource(t, ctx, ti, origin)
	require.Equal(t, origin+wellknown.OAuthProtectedResourcePath, failed.MetadataUrl.String)
	require.Contains(t, failed.MetadataLastError.String, "not advertised")
	require.True(t, failed.MetadataLastErrorAt.Valid)
	require.False(t, failed.MetadataFetchedAt.Valid)
	require.Nil(t, failed.Metadata)
	require.False(t, failed.ResourceName.Valid)

	available.Store(true)
	updateServerURL(t, ctx, ti, server, origin)

	recovered := loadProtectedResource(t, ctx, ti, origin)
	require.Equal(t, failed.ID, recovered.ID, "the same row is updated in place")
	require.Equal(t, "Resource B", recovered.ResourceName.String)
	require.Equal(t, []string{"read"}, recovered.ScopesSupported)
	require.True(t, recovered.MetadataFetchedAt.Valid)
	require.False(t, recovered.MetadataLastError.Valid)
	require.False(t, recovered.MetadataLastErrorAt.Valid)
	require.True(t, recovered.UpdatedAt.Time.After(failed.UpdatedAt.Time))
}

// Operator-driven discovery lands in the table as well.
func TestDiscoverProtectedResourceMetadata_RecordsProtectedResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	metadata := &wellknown.OAuthProtectedResourceMetadata{
		Resource:               "",
		AuthorizationServers:   []string{"https://auth.example.com"},
		ScopesSupported:        []string{"read", "write"},
		BearerMethodsSupported: []string{"header"},
		ResourceDocumentation:  "https://docs.example.com",
		ResourceName:           "Example",
		ResourcePolicyURI:      "",
		ResourceTosURI:         "",
		Raw:                    nil,
		MetadataURL:            "",
	}
	upstream := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{Metadata: metadata, StatusCode: 0, Body: nil})
	metadata.Resource = upstream.URL
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: server.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.True(t, out.Available)

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, []string{"read", "write"}, row.ScopesSupported)
	require.Equal(t, "Example", row.ResourceName.String)
	require.Equal(t, upstream.URL+wellknown.OAuthProtectedResourcePath, row.MetadataUrl.String)
	require.True(t, row.MetadataFetchedAt.Valid)
}

// A typed probe failure on operator-driven discovery records the reason.
func TestDiscoverProtectedResourceMetadata_RecordsFetchError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, getProjectID(t, ctx)))

	upstream := oauthtest.LaunchProtectedResourceServer(t, oauthtest.ProtectedResourceServerOpts{Metadata: nil, StatusCode: 0, Body: nil})
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)

	out, err := ti.service.DiscoverProtectedResourceMetadata(ctx, &gen.DiscoverProtectedResourceMetadataPayload{
		RemoteMcpServerID: server.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)
	require.False(t, out.Available)

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, out.Unavailable.Message, row.MetadataLastError.String)
	require.True(t, row.MetadataLastErrorAt.Valid)
	require.False(t, row.MetadataFetchedAt.Valid)
}

// rejectingUpstream serves a 401 whose challenge carries the given
// WWW-Authenticate value.
func rejectingUpstream(t *testing.T, wwwAuthenticate string) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", wwwAuthenticate)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	t.Cleanup(upstream.Close)
	return upstream
}

func postInitialize(t *testing.T, ctx context.Context, manager *remotemcp.ProxyManager, server repo.RemoteMcpServer) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	p := manager.Build(testenv.NewLogger(t), &server, uuid.NewString(), nil, mcpservers.VisibilityPublic, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), "", "", nil, remotemcp.WithoutToolsCallIdentityCoverage())

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x/mcp/id", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	require.NoError(t, p.Post(rr, req))
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

// An upstream challenge's scope param lands on the resource row; a challenge
// without one records nothing, and a repeat of the same scopes does not
// rewrite the row.
func TestProxyManager_RecordsChallengeScopes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	manager := remotemcp.NewProxyManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	written := make(chan struct{}, 8)
	manager.SetAfterChallengeScopes(func() { written <- struct{}{} })

	withScopes := rejectingUpstream(t, `Bearer realm="mcp", scope="a b", error="insufficient_scope"`)
	withoutScopes := rejectingUpstream(t, `Bearer realm="mcp", error="invalid_token"`)

	scoped := seedRemoteMcpServerWithURL(t, ctx, ti, withScopes.URL)
	unscoped := seedRemoteMcpServerWithURL(t, ctx, ti, withoutScopes.URL)
	for _, resourceURL := range []string{withScopes.URL, withoutScopes.URL} {
		_, err := repo.New(ti.conn).UpsertRemoteProtectedResource(ctx, repo.UpsertRemoteProtectedResourceParams{
			ProjectID:              *authCtx.ProjectID,
			OrganizationID:         authCtx.ActiveOrganizationID,
			ResourceIdentifier:     resourceURL,
			MetadataUrl:            "",
			AuthorizationServers:   []string{"https://auth.example.test"},
			ScopesSupported:        nil,
			BearerMethodsSupported: nil,
			ResourceName:           "",
			ResourceDocumentation:  "",
			ResourcePolicyUri:      "",
			ResourceTosUri:         "",
			Metadata:               "",
		})
		require.NoError(t, err)
	}

	postInitialize(t, ctx, manager, scoped)
	<-written
	first := loadProtectedResource(t, ctx, ti, withScopes.URL)
	require.Equal(t, []string{"a", "b"}, first.ChallengeScopes)
	require.True(t, first.ChallengeScopesSeenAt.Valid)

	postInitialize(t, ctx, manager, unscoped)
	none := loadProtectedResource(t, ctx, ti, withoutScopes.URL)
	require.Nil(t, none.ChallengeScopes)
	require.False(t, none.ChallengeScopesSeenAt.Valid)

	postInitialize(t, ctx, manager, scoped)
	<-written
	repeat := loadProtectedResource(t, ctx, ti, withScopes.URL)
	require.Equal(t, first.ChallengeScopesSeenAt.Time, repeat.ChallengeScopesSeenAt.Time, "identical scopes are not rewritten")
	require.Equal(t, first.UpdatedAt.Time, repeat.UpdatedAt.Time)
}

// Back-to-back challenges with different scopes leave the row holding the
// latest one, whichever detached write finishes first.
func TestProxyManager_ChallengeScopesLatestWins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	manager := remotemcp.NewProxyManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	written := make(chan struct{}, 8)
	manager.SetAfterChallengeScopes(func() { written <- struct{}{} })

	scopeA, scopeB := `Bearer scope="a"`, `Bearer scope="b"`
	var challenge atomic.Pointer[string]
	challenge.Store(&scopeA)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", *challenge.Load())
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	_, err = repo.New(ti.conn).UpsertRemoteProtectedResource(ctx, repo.UpsertRemoteProtectedResourceParams{
		ProjectID:              *authCtx.ProjectID,
		OrganizationID:         authCtx.ActiveOrganizationID,
		ResourceIdentifier:     upstream.URL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{"https://auth.example.test"},
		ScopesSupported:        nil,
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)

	postInitialize(t, ctx, manager, server)
	challenge.Store(&scopeB)
	postInitialize(t, ctx, manager, server)
	<-written
	<-written

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, []string{"b"}, row.ChallengeScopes)
}

// A challenge for a resource without a row records nothing and does not
// fail the relay, but retries once the on-use probe creates the row.
func TestProxyManager_ChallengeScopesWithoutRowIsNoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	manager := remotemcp.NewProxyManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	written := make(chan struct{}, 1)
	manager.SetAfterChallengeScopes(func() { written <- struct{}{} })
	probed := make(chan struct{}, 1)
	manager.SetAfterProtectedResourceProbe(func() { probed <- struct{}{} })

	// The on-use probe is held until the challenge is handled, so no row exists yet.
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == wellknown.OAuthProtectedResourcePath {
			<-release
			http.NotFound(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer scope="a"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	postInitialize(t, ctx, manager, server)
	<-written

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: upstream.URL})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	close(release)
	<-probed
	before := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Nil(t, before.ChallengeScopes)

	postInitialize(t, ctx, manager, server)
	<-written
	first := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, []string{"a"}, first.ChallengeScopes)
	require.True(t, first.ChallengeScopesSeenAt.Valid)

	postInitialize(t, ctx, manager, server)
	<-written
	repeat := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, first.ChallengeScopesSeenAt.Time, repeat.ChallengeScopesSeenAt.Time, "identical scopes are not rewritten")
	require.Equal(t, first.UpdatedAt.Time, repeat.UpdatedAt.Time)
}

// probedUpstream rejects MCP traffic with a bare 401 and serves body at the
// origin-style well-known path ("{{origin}}" is its own URL; an empty body is
// a 404). The counter is the number of metadata reads.
func probedUpstream(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var origin string
	hits := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		hits.Add(1)
		if body == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.ReplaceAll(body, "{{origin}}", origin)))
	}))
	t.Cleanup(upstream.Close)
	origin = upstream.URL
	return upstream, hits
}

// newProbingManager is a fresh replica: its own debounce state, and a channel
// signalled each time a detached on-use probe finishes.
func newProbingManager(t *testing.T, ti *testInstance) (*remotemcp.ProxyManager, <-chan struct{}) {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	manager := remotemcp.NewProxyManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	probed := make(chan struct{}, 8)
	manager.SetAfterProtectedResourceProbe(func() { probed <- struct{}{} })
	return manager, probed
}

const probedResourceDocument = `{"resource":"{{origin}}","authorization_servers":["https://auth.example.test"],"scopes_supported":["read"],"resource_name":"Probed"}`

// The first proxied request to a server without a row records its metadata.
func TestProxyManager_ProbesProtectedResourceOnFirstUse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	upstream, hits := probedUpstream(t, probedResourceDocument)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	manager, probed := newProbingManager(t, ti)

	postInitialize(t, ctx, manager, server)
	<-probed

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, authCtx.ActiveOrganizationID, row.OrganizationID)
	require.Equal(t, upstream.URL+wellknown.OAuthProtectedResourcePath, row.MetadataUrl.String)
	require.Equal(t, []string{"https://auth.example.test"}, row.AuthorizationServers)
	require.Equal(t, []string{"read"}, row.ScopesSupported)
	require.Equal(t, "Probed", row.ResourceName.String)
	require.True(t, row.MetadataFetchedAt.Valid)
	require.False(t, row.MetadataLastError.Valid)
	require.EqualValues(t, 1, hits.Load())
}

// A replica that checked a server within the hour neither probes nor reads
// the row again; once the hour passes it reads the row and finds it fresh.
func TestProxyManager_ProtectedResourceProbeDebouncedPerReplica(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	upstream, hits := probedUpstream(t, probedResourceDocument)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	manager, probed := newProbingManager(t, ti)
	var clock atomic.Int64
	manager.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(time.Duration(clock.Load())) })

	postInitialize(t, ctx, manager, server)
	<-probed
	first := loadProtectedResource(t, ctx, ti, upstream.URL)

	postInitialize(t, ctx, manager, server)
	require.Empty(t, probed, "a debounced use starts no detached work")
	require.EqualValues(t, 1, hits.Load())

	clock.Store(int64(2 * time.Hour))
	postInitialize(t, ctx, manager, server)
	<-probed
	require.EqualValues(t, 1, hits.Load(), "a row read two hours ago is still fresh")
	require.Equal(t, first.UpdatedAt.Time, loadProtectedResource(t, ctx, ti, upstream.URL).UpdatedAt.Time)
}

// A row read within the day is not probed again by another replica; one read
// longer ago is.
func TestProxyManager_ProtectedResourceProbeFollowsRowFreshness(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	upstream, hits := probedUpstream(t, probedResourceDocument)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)

	first, firstProbed := newProbingManager(t, ti)
	postInitialize(t, ctx, first, server)
	<-firstProbed
	fetched := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.EqualValues(t, 1, hits.Load())

	fresh, freshProbed := newProbingManager(t, ti)
	postInitialize(t, ctx, fresh, server)
	<-freshProbed
	require.EqualValues(t, 1, hits.Load())
	require.Equal(t, fetched.MetadataFetchedAt.Time, loadProtectedResource(t, ctx, ti, upstream.URL).MetadataFetchedAt.Time)

	// A replica whose clock runs a day ahead sees the same row as stale.
	later, laterProbed := newProbingManager(t, ti)
	later.SetProtectedResourceProbeClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
	postInitialize(t, ctx, later, server)
	<-laterProbed
	require.EqualValues(t, 2, hits.Load())
	require.True(t, loadProtectedResource(t, ctx, ti, upstream.URL).MetadataFetchedAt.Time.After(fetched.MetadataFetchedAt.Time))
}

// A failed probe records the public-safe reason, and the failure keeps other
// replicas from probing again within the day.
func TestProxyManager_FailedProtectedResourceProbeIsRecordedAndNotRetried(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	upstream, hits := probedUpstream(t, "")
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)

	first, firstProbed := newProbingManager(t, ti)
	postInitialize(t, ctx, first, server)
	<-firstProbed
	failed := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, upstream.URL+wellknown.OAuthProtectedResourcePath, failed.MetadataUrl.String)
	require.Contains(t, failed.MetadataLastError.String, "not advertised")
	require.True(t, failed.MetadataLastErrorAt.Valid)
	require.False(t, failed.MetadataFetchedAt.Valid)
	require.EqualValues(t, 1, hits.Load())

	second, secondProbed := newProbingManager(t, ti)
	postInitialize(t, ctx, second, server)
	<-secondProbed
	require.EqualValues(t, 1, hits.Load())
	require.Equal(t, failed.MetadataLastErrorAt.Time, loadProtectedResource(t, ctx, ti, upstream.URL).MetadataLastErrorAt.Time)
}

// A document naming another resource is recorded as unusable, with nothing it advertised.
func TestProxyManager_ProtectedResourceProbeRecordsAnotherResourceAsError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	upstream, hits := probedUpstream(t, `{"resource":"https://other.example.test/mcp","authorization_servers":["https://auth.example.test"]}`)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	manager, probed := newProbingManager(t, ti)

	postInitialize(t, ctx, manager, server)
	<-probed
	require.EqualValues(t, 1, hits.Load())

	row := loadProtectedResource(t, ctx, ti, upstream.URL)
	require.Equal(t, "The metadata document resource or location does not match the requested resource.", row.MetadataLastError.String)
	require.True(t, row.MetadataLastErrorAt.Valid)
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Nil(t, row.AuthorizationServers)

	postInitialize(t, ctx, manager, server)
	require.Empty(t, probed)
	require.EqualValues(t, 1, hits.Load(), "the check still counts for the debounce")
}
