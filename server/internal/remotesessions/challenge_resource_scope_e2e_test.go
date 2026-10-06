// End-to-end coverage of resource-first scope resolution: the protected
// resource row of the MCP server a login is for, probed live within a budget,
// decides the scope request ahead of the issuer's catalogue.

package remotesessions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// seedRemoteMcpServerForLogin binds a remote-backed MCP server at serverURL
// to the login's user session issuer and returns its id.
func seedRemoteMcpServerForLogin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, userIssuerID uuid.UUID, slug, serverURL string) uuid.UUID {
	t.Helper()
	remoteServer, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           serverURL,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(userIssuerID),
	})
	require.NoError(t, err)
	return server.ID
}

// seedProtectedResource writes the row a login would find for resourceURL.
func seedProtectedResource(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, resourceURL string, seed protectedResourceSeed) {
	t.Helper()
	q := remotemcprepo.New(conn)
	_, err := q.UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID:              projectID,
		OrganizationID:         organizationID,
		ResourceIdentifier:     resourceURL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{syntheticIssuerURL},
		ScopesSupported:        seed.scopesSupported,
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
	now := time.Now()
	fetchedAt := conv.ToPGTimestamptz(now.Add(-seed.fetchedAgo))
	errorAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if seed.errorAgo != 0 {
		errorAt = conv.ToPGTimestamptz(now.Add(-seed.errorAgo))
	}
	rows, err := q.SetRemoteProtectedResourceMetadataTimestamps(ctx, remotemcprepo.SetRemoteProtectedResourceMetadataTimestampsParams{
		MetadataFetchedAt:   fetchedAt,
		MetadataLastErrorAt: errorAt,
		ProjectID:           projectID,
		ResourceIdentifier:  resourceURL,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	if seed.pin != nil {
		rows, err = q.SetRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.SetRemoteProtectedResourceScopeOverrideParams{ScopeOverride: seed.pin, ProjectID: projectID, ResourceIdentifier: resourceURL})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}
	if seed.challengeScopes != nil {
		rows, err = q.RecordRemoteProtectedResourceChallengeScopes(ctx, remotemcprepo.RecordRemoteProtectedResourceChallengeScopesParams{ChallengeScopes: seed.challengeScopes, ProjectID: projectID, ResourceIdentifier: resourceURL})
		require.NoError(t, err)
		require.EqualValues(t, 1, rows)
	}
}

// fakeResource is a remote MCP server whose well-known document advertises
// scopes; nil scopes serve a 404. hits counts metadata reads.
func fakeResource(t *testing.T, scopes []string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellknown.OAuthProtectedResourcePath {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		hits.Add(1)
		if scopes == nil {
			http.NotFound(w, r)
			return
		}
		// Serialized here so an empty list reads as [] rather than [""].
		advertised, err := json.Marshal(scopes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":"http://` + r.Host + `","authorization_servers":["` + syntheticIssuerURL + `"],"scopes_supported":` + string(advertised) + `}`))
	}))
	t.Cleanup(server.Close)
	return server, hits
}

func loadResourceRow(t *testing.T, ctx context.Context, env syntheticExpiryEnv, resourceURL string) remotemcprepo.RemoteProtectedResource {
	t.Helper()
	row, err := remotemcprepo.New(env.db).GetRemoteProtectedResource(ctx, remotemcprepo.GetRemoteProtectedResourceParams{ProjectID: env.projectID, ResourceIdentifier: resourceURL})
	require.NoError(t, err)
	return row
}

// scopeResolutionAttributes returns the attribute set of the one scope
// resolution the manager's meter recorded.
func scopeResolutionAttributes(t *testing.T, env syntheticExpiryEnv) attribute.Set {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, env.registrationTelemetryReader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gram.remote_session.scope_resolution" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.EqualValues(t, 1, sum.DataPoints[0].Value)
			return sum.DataPoints[0].Attributes
		}
	}
	require.Fail(t, "scope resolution was not recorded")
	return attribute.Set{}
}

func requireScopeResolution(t *testing.T, env syntheticExpiryEnv, source remotesessionmetrics.ScopeSource, outcome protectedresource.ProbeOutcome) {
	t.Helper()
	set := scopeResolutionAttributes(t, env)
	got, _ := set.Value(attr.OAuthScopeSourceKey)
	require.Equal(t, string(source), got.AsString())
	got, _ = set.Value(attr.OAuthResourceProbeOutcomeKey)
	require.Equal(t, string(outcome), got.AsString())
}

// The client's stored scope beats everything the resource row says.
func TestRemoteLogin_ClientScopeBeatsPin(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	_, env := newSyntheticExpiryEnv(t, "client-beats-pin", scopelessToken,
		withIssuerScopes("channels:history", "openid", "offline_access"),
		withClientScope("channels:history"),
		withScopeOverride("custom:one"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, challengeScopes: []string{"files:write"}, fetchedAgo: 2 * time.Hour}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "channels:history openid offline_access", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load(), "a stale row is still refreshed while the client scope decides")
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceClientScope, protectedresource.ProbeOutcomeFetched)
}

// The resource's last challenge beats the operator's pin.
func TestRemoteLogin_ChallengeScopesBeatPin(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	_, env := newSyntheticExpiryEnv(t, "challenge-beats-pin", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, challengeScopes: []string{"files:write"}, fetchedAgo: 10 * time.Minute}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:write openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load(), "the row is refreshed even though the challenge decides")
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceChallengeScope, protectedresource.ProbeOutcomeFetched)
}

// The pin beats the live list and the issuer override, gains the standard
// scopes the issuer advertises, and keeps pinned scopes the list lacks.
func TestRemoteLogin_PinBeatsLiveList(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	_, env := newSyntheticExpiryEnv(t, "pin-beats-list", scopelessToken,
		withIssuerScopes("admin", "openid", "offline_access"),
		withScopeOverride("custom:one"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read", "legacy:all"}, fetchedAgo: 2 * time.Hour}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read legacy:all openid offline_access", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceResourcePin, protectedresource.ProbeOutcomeFetched)
}

// The resource's last challenge names the scopes it wants; the advertised list yields to it.
func TestRemoteLogin_ChallengeScopesBeatLiveList(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	_, env := newSyntheticExpiryEnv(t, "challenge-scopes", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{challengeScopes: []string{"files:read"}, fetchedAgo: 2 * time.Hour}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceChallengeScope, protectedresource.ProbeOutcomeFetched)
}

// A login for a server with no row probes it, records the document, and
// requests what it advertises.
func TestRemoteLogin_LiveProbeFillsRowAndDecides(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	ctx, env := newSyntheticExpiryEnv(t, "live-probe", scopelessToken,
		withIssuerScopes("admin", "openid", "offline_access"),
		withRemoteServer(resource.URL),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read files:write openid offline_access", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	row := loadResourceRow(t, ctx, env, resource.URL)
	require.Equal(t, []string{"files:read", "files:write"}, row.ScopesSupported)
	require.True(t, row.MetadataFetchedAt.Valid)
	require.Equal(t, resource.URL+wellknown.OAuthProtectedResourcePath, row.MetadataUrl.String)
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceLiveResource, protectedresource.ProbeOutcomeFetched)
}

// A row read moments ago is still probed: the resource may have changed what
// it advertises, so the live list wins over the cached one.
func TestRemoteLogin_RecentRowStillProbes(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:write"})
	_, env := newSyntheticExpiryEnv(t, "recent-row", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{scopesSupported: []string{"files:read"}, fetchedAgo: 10 * time.Minute}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:write openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceLiveResource, protectedresource.ProbeOutcomeFetched)
}

// A probe that fails falls back to the row's last good list and records the failure.
func TestRemoteLogin_ProbeFailureFallsBackToCachedRow(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, nil)
	ctx, env := newSyntheticExpiryEnv(t, "probe-failure", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{scopesSupported: []string{"files:read"}, fetchedAgo: 2 * 24 * time.Hour}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	row := loadResourceRow(t, ctx, env, resource.URL)
	require.True(t, row.MetadataLastErrorAt.Valid, "the failed read is recorded")
	require.Equal(t, []string{"files:read"}, row.ScopesSupported, "the last good document stands")
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceCachedResource, protectedresource.ProbeOutcomeError)
}

// A failure recorded minutes ago is not retried; the cached list still serves.
func TestRemoteLogin_RecentFailureSkipsProbe(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read"})
	_, env := newSyntheticExpiryEnv(t, "recent-failure", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{scopesSupported: []string{"files:read"}, fetchedAgo: 2 * time.Hour, errorAgo: 5 * time.Minute}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 0, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceCachedResource, protectedresource.ProbeOutcomeSkippedRecentError)
}

// A cached list older than a week says nothing; the issuer's catalogue is requested.
func TestRemoteLogin_CachedRowOlderThanAWeekIsIgnored(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, nil)
	_, env := newSyntheticExpiryEnv(t, "old-row", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{scopesSupported: []string{"files:read"}, fetchedAgo: 8 * 24 * time.Hour}),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "admin openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceIssuerCatalogue, protectedresource.ProbeOutcomeError)
}

// A login with no remote-backed server has no resource row to consult.
func TestRemoteLogin_NoServerSkipsResourceSteps(t *testing.T) {
	t.Parallel()

	_, env := newSyntheticExpiryEnv(t, "no-server", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "admin openid", scopeOf(t, env.authURL))
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceIssuerCatalogue, protectedresource.ProbeOutcomeNotApplicable)
}

// Without the flag the resource row is ignored and nothing is probed.
func TestRemoteLogin_DiscoveryFlagOffKeepsLegacyResolution(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read"})
	_, env := newSyntheticExpiryEnv(t, "flag-off", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, scopesSupported: []string{"files:read"}, challengeScopes: []string{"files:read"}, fetchedAgo: 48 * time.Hour}),
		withProber(), withRegistrationTelemetry(),
	)
	require.Equal(t, "admin openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 0, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceIssuerCatalogue, protectedresource.ProbeOutcomeNotApplicable)
}

// AIM-288: an issuer with a large catalogue, a client with no scope, and a
// resource advertising three scopes requests exactly those three plus the
// standard scopes the issuer advertises.
func TestRemoteLogin_ResourceListReplacesLargeIssuerCatalogue(t *testing.T) {
	t.Parallel()

	catalogue := make([]string, 0, 44)
	for i := range 40 {
		catalogue = append(catalogue, fmt.Sprintf("catalogue:%02d", i))
	}
	catalogue = append(catalogue, "openid", "email", "profile", "offline_access")
	resource, hits := fakeResource(t, []string{"files:read", "files:write", "projects:read"})
	_, env := newSyntheticExpiryEnv(t, "aim-288", scopelessToken,
		withIssuerScopes(catalogue...),
		withRemoteServer(resource.URL),
		withProber(), withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "files:read files:write projects:read openid email profile offline_access", scopeOf(t, env.authURL))
	require.EqualValues(t, 1, hits.Load())
	require.Equal(t, []string{"files:read", "files:write", "projects:read", "openid", "email", "profile", "offline_access"}, env.session.Scopes)
}

// The consent card reads the cached row through the client listing and never probes.
func TestListClients_ReadsCachedResourceRowWithoutProbing(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read", "files:write"})
	ctx, env := newSyntheticExpiryEnv(t, "cached-card", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, challengeScopes: []string{"c"}, scopesSupported: []string{"files:read", "files:write"}, fetchedAgo: 10 * time.Minute}),
		withProber(), withLiveResourceScopes(),
	)
	require.EqualValues(t, 1, hits.Load(), "the login itself probes once")

	// The consent card reads the row by the login's server, with or without
	// the client claiming the resource.
	byServer, byServerURL, ok := env.mgr.CachedResourceScopesForServer(ctx, env.projectID, env.mcpServerID, true)
	require.True(t, ok)
	require.Equal(t, resource.URL, byServerURL)
	require.Equal(t, []string{"files:read"}, byServer.Pin)
	require.Equal(t, []string{"c"}, byServer.ChallengeScopes)
	require.Equal(t, []string{"files:read", "files:write"}, byServer.ScopesSupported)
	require.False(t, byServer.Live)
	require.True(t, byServer.UseDiscovered)
	_, _, ok = env.mgr.CachedResourceScopesForServer(ctx, env.projectID, uuid.NullUUID{}, true)
	require.False(t, ok, "a login with no server has no row")
	require.EqualValues(t, 1, hits.Load())

	// The client claims the resource, as the Platform MCP attachment records it.
	_, err := env.q.UpdateRemoteSessionClientResourceDisplay(ctx, repo.UpdateRemoteSessionClientResourceDisplayParams{
		ResourceIdentifier:    conv.ToPGText(resource.URL),
		ResourceName:          "",
		ResourceDocumentation: "",
		ResourcePolicyUri:     "",
		ResourceTosUri:        "",
		ID:                    env.clientID,
		ProjectID:             env.projectID,
		OrganizationID:        env.organizationID,
	})
	require.NoError(t, err)

	clients, err := env.mgr.ListClients(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	c := clients[0]
	require.Equal(t, []string{"files:read"}, c.ResourceScopeOverride)
	require.Equal(t, []string{"c"}, c.ResourceChallengeScopes)
	require.Equal(t, []string{"files:read", "files:write"}, c.ResourceScopesSupported)
	require.True(t, env.mgr.ResourceScopeDiscoveryEnabled(ctx, env.organizationID))
	require.Equal(t, []string{"c", "openid"}, c.RequestedScopes(c.CachedResourceScopes(true)).Scopes)
	require.Equal(t, []string{"admin", "openid"}, c.RequestedScopes(c.CachedResourceScopes(false)).Scopes)
	require.EqualValues(t, 1, hits.Load(), "listing clients never probes")
}

// Without a prober the manager keeps today's behaviour whatever the row says.
func TestRemoteLogin_NoProberIgnoresResourceRow(t *testing.T) {
	t.Parallel()

	resource, hits := fakeResource(t, []string{"files:read"})
	_, env := newSyntheticExpiryEnv(t, "no-prober", scopelessToken,
		withIssuerScopes("admin", "openid"),
		withRemoteServer(resource.URL),
		withProtectedResource(protectedResourceSeed{pin: []string{"files:read"}, fetchedAgo: 10 * time.Minute}),
		withLiveResourceScopes(), withRegistrationTelemetry(),
	)
	require.Equal(t, "admin openid", scopeOf(t, env.authURL))
	require.EqualValues(t, 0, hits.Load())
	requireScopeResolution(t, env, remotesessionmetrics.ScopeSourceIssuerCatalogue, protectedresource.ProbeOutcomeNotApplicable)
}
