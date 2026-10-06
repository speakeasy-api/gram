package oktaresourceconnections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// Readiness reports the scopes a login would request from the cached
// resource row and never probes the resource itself.
func TestList_ReadsCachedResourceScopesWithoutProbing(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	si.flags.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, si.orgID, true)
	recordAgent(t, ctx, si, "wlp1")

	hits := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	// A client with no scope of its own leaves the decision to the resource.
	_, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "0oascopeless",
		Scope:                 []string{},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText("Probed"), Slug: conv.ToPGText("probed-" + uuid.NewString()[:8]), Url: upstream.URL})
	require.NoError(t, err)
	serverID, err := si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{
		ProjectID:             projectID,
		Name:                  conv.ToPGText("Probed"),
		Slug:                  conv.ToPGText("probed-" + uuid.NewString()[:8]),
		RemoteMcpServerID:     uuid.NullUUID{UUID: backendID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		UserSessionIssuerID:   uuid.NullUUID{},
		Visibility:            "private",
	})
	require.NoError(t, err)

	// No row yet: the issuer's catalogue stands, and nothing is probed.
	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load())

	rq := remotemcprepo.New(si.conn)
	_, err = rq.UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID:              projectID,
		OrganizationID:         si.orgID,
		ResourceIdentifier:     upstream.URL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{audience},
		ScopesSupported:        []string{"files:read", "files:write"},
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"files:read", "files:write"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)

	n, err := rq.SetRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.SetRemoteProtectedResourceScopeOverrideParams{ScopeOverride: []string{"legacy:all"}, ProjectID: projectID, ResourceIdentifier: upstream.URL})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, []string{"legacy:all"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)

	// Not enrolled: the row is ignored again.
	si.flags.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, si.orgID, false)
	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}

// scopelessRemoteServer is an eligible remote-backed server whose issuer has
// one client with no scope of its own, so the resource row decides. hits
// counts every request the upstream receives.
func scopelessRemoteServer(t *testing.T, ctx context.Context, si *instance) (projectID, serverID uuid.UUID, upstreamURL string, hits *atomic.Int32) {
	t.Helper()
	si.flags.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, si.orgID, true)
	recordAgent(t, ctx, si, "wlp-"+uuid.NewString()[:8])

	hits = new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	projectID = createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	_, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "0oascopeless-" + uuid.NewString()[:8],
		Scope:                 []string{},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	slug := "cached-" + uuid.NewString()[:8]
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText("Cached"), Slug: conv.ToPGText(slug), Url: upstream.URL})
	require.NoError(t, err)
	serverID, err = si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{
		ProjectID:             projectID,
		Name:                  conv.ToPGText("Cached"),
		Slug:                  conv.ToPGText(slug),
		RemoteMcpServerID:     uuid.NullUUID{UUID: backendID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		UserSessionIssuerID:   uuid.NullUUID{},
		Visibility:            "private",
	})
	require.NoError(t, err)
	return projectID, serverID, upstream.URL, hits
}

// cacheAdvertisedScopes writes the resource row with the advertised list,
// read fetchedAgo ago; a zero fetchedAgo leaves the read time unset.
func cacheAdvertisedScopes(t *testing.T, ctx context.Context, si *instance, projectID uuid.UUID, upstreamURL string, scopes []string, fetchedAgo time.Duration) {
	t.Helper()
	rq := remotemcprepo.New(si.conn)
	_, err := rq.UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID:              projectID,
		OrganizationID:         si.orgID,
		ResourceIdentifier:     upstreamURL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{audience},
		ScopesSupported:        scopes,
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
	fetchedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if fetchedAgo != 0 {
		fetchedAt = conv.ToPGTimestamptz(time.Now().Add(-fetchedAgo))
	}
	n, err := rq.SetRemoteProtectedResourceMetadataTimestamps(ctx, remotemcprepo.SetRemoteProtectedResourceMetadataTimestampsParams{
		MetadataFetchedAt:   fetchedAt,
		MetadataLastErrorAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ProjectID:           projectID,
		ResourceIdentifier:  upstreamURL,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

// An advertised list read within the week is what a login would request.
func TestList_FreshCachedScopesDecide(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID, serverID, upstreamURL, hits := scopelessRemoteServer(t, ctx, si)

	cacheAdvertisedScopes(t, ctx, si, projectID, upstreamURL, []string{"files:read", "files:write"}, 6*24*time.Hour)

	require.Equal(t, []string{"files:read", "files:write"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}

// A login falls through to the issuer when the cached list is older than a
// week; readiness must agree rather than report the obsolete list.
func TestList_CachedScopesOlderThanAWeekFallThrough(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID, serverID, upstreamURL, hits := scopelessRemoteServer(t, ctx, si)

	cacheAdvertisedScopes(t, ctx, si, projectID, upstreamURL, []string{"files:read", "files:write"}, 8*24*time.Hour)

	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}

// A row whose list was never dated by a read says nothing about the resource.
func TestList_CachedScopesWithoutReadTimeFallThrough(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID, serverID, upstreamURL, hits := scopelessRemoteServer(t, ctx, si)

	cacheAdvertisedScopes(t, ctx, si, projectID, upstreamURL, []string{"files:read", "files:write"}, 0)

	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}

// The operator's pin and the resource's last challenge do not expire with
// the advertised list: a stale row still carries them, and the challenge
// beats the pin as it does at login.
func TestList_StaleCachedScopesKeepPinAndChallenge(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID, serverID, upstreamURL, hits := scopelessRemoteServer(t, ctx, si)
	rq := remotemcprepo.New(si.conn)

	cacheAdvertisedScopes(t, ctx, si, projectID, upstreamURL, []string{"files:read", "files:write"}, 8*24*time.Hour)
	n, err := rq.SetRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.SetRemoteProtectedResourceScopeOverrideParams{ScopeOverride: []string{"legacy:all"}, ProjectID: projectID, ResourceIdentifier: upstreamURL})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, []string{"legacy:all"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)

	n, err = rq.RecordRemoteProtectedResourceChallengeScopes(ctx, remotemcprepo.RecordRemoteProtectedResourceChallengeScopesParams{ChallengeScopes: []string{"challenge:read"}, ProjectID: projectID, ResourceIdentifier: upstreamURL})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, []string{"challenge:read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}
