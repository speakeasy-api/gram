package remotemcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
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

// A challenge for a resource without a row records nothing and does not
// fail the relay.
func TestProxyManager_ChallengeScopesWithoutRowIsNoop(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceForProbe(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	manager := remotemcp.NewProxyManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, policy, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	written := make(chan struct{}, 1)
	manager.SetAfterChallengeScopes(func() { written <- struct{}{} })

	upstream := rejectingUpstream(t, `Bearer scope="a"`)
	server := seedRemoteMcpServerWithURL(t, ctx, ti, upstream.URL)
	postInitialize(t, ctx, manager, server)
	<-written

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err = repo.New(ti.conn).GetRemoteProtectedResource(ctx, repo.GetRemoteProtectedResourceParams{ProjectID: *authCtx.ProjectID, ResourceIdentifier: upstream.URL})
	require.Error(t, err)
}
