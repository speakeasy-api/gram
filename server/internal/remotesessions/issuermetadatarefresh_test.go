package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newIssuerMetadataRefresher builds the refresher over the test database with a manual metric reader.
func newIssuerMetadataRefresher(t *testing.T, ti *testInstance) (*remotesessions.IssuerMetadataRefresher, *sdkmetric.ManualReader) {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return remotesessions.NewIssuerMetadataRefresher(testenv.NewLogger(t), provider, ti.conn, policy, audit.NewLogger()), reader
}

// metadataTracking is the tracking state a test stamps on an issuer row; a nil errorAt with an error means now.
type metadataTracking struct {
	document  string
	fetchedAt *time.Time
	lastError string
	errorAt   *time.Time
	errorURL  string
}

func setIssuerMetadataTracking(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, tracking metadataTracking) {
	t.Helper()
	row := loadIssuerByID(t, ctx, ti, id)

	var fetchedAt, errorAt pgtype.Timestamptz
	if tracking.fetchedAt != nil {
		fetchedAt = conv.ToPGTimestamptz(*tracking.fetchedAt)
	}
	if tracking.lastError != "" {
		errorAt = conv.ToPGTimestamptz(conv.PtrValOr(tracking.errorAt, time.Now()))
	}
	require.NoError(t, repo.New(ti.conn).SetRemoteSessionIssuerMetadataTracking(ctx, repo.SetRemoteSessionIssuerMetadataTrackingParams{
		Metadata:             tracking.document,
		MetadataFetchedAt:    fetchedAt,
		MetadataLastError:    tracking.lastError,
		MetadataLastErrorAt:  errorAt,
		MetadataLastErrorUrl: tracking.errorURL,
		ID:                   row.ID,
		ProjectID:            row.ProjectID,
		OrganizationID:       row.OrganizationID,
	}))
}

func loadIssuerByID(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) repo.RemoteSessionIssuer {
	t.Helper()
	row, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDUnscoped(ctx, id)
	require.NoError(t, err)
	return row
}

// refreshCandidate builds the sweep's view of a stored row.
func refreshCandidate(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) remotesessions.IssuerMetadataRefreshCandidate {
	t.Helper()
	row := loadIssuerByID(t, ctx, ti, id)
	return remotesessions.IssuerMetadataRefreshCandidate{
		ID:                  row.ID,
		IssuerURL:           row.Issuer,
		Host:                remotesessions.IssuerMetadataRefreshHost(row.Issuer),
		ProjectID:           row.ProjectID,
		OrganizationID:      row.OrganizationID,
		TunneledMcpServerID: row.TunneledMcpServerID,
	}
}

func createProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug, issuerURL string) uuid.UUID {
	t.Helper()
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL(slug, issuerURL))
	require.NoError(t, err)
	return uuid.MustParse(created.ID)
}

func dueIDs(t *testing.T, ctx context.Context, refresher *remotesessions.IssuerMetadataRefresher, now time.Time) []uuid.UUID {
	t.Helper()
	candidates, _, err := refresher.ListDue(ctx, now, nil, 1000)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

func reprojectableIDs(t *testing.T, ctx context.Context, refresher *remotesessions.IssuerMetadataRefresher, now time.Time) []uuid.UUID {
	t.Helper()
	candidates, err := refresher.ListReprojectable(ctx, now, 1000)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}

// statusServer answers every path with one status and no body.
func statusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

// partialIssuerServer fails the RFC 8414 location transiently and serves the document at the OpenID one.
func partialIssuerServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/openid-configuration") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"scopes_supported":                      []string{"openid"},
			"grant_types_supported":                 []string{"authorization_code"},
			"response_types_supported":              []string{"code"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func outcomeCounts(t *testing.T, reader *sdkmetric.ManualReader) map[remotesessionmetrics.IssuerMetadataRefreshOutcome]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[remotesessionmetrics.IssuerMetadataRefreshOutcome]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session_issuer.metadata_refresh" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				outcome, _ := dp.Attributes.Value(attr.OutcomeKey)
				counts[remotesessionmetrics.IssuerMetadataRefreshOutcome(outcome.AsString())] += dp.Value
			}
		}
	}
	return counts
}

func TestIssuerMetadataRefresh_ListDue_Predicate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	now := time.Now()
	ago := func(d time.Duration) *time.Time { ts := now.Add(-d); return &ts }

	neverFetched := createProjectIssuer(t, ctx, ti, "due-never", "https://never.example.com")
	stale := createProjectIssuer(t, ctx, ti, "due-stale", "https://stale.example.com")
	setIssuerMetadataTracking(t, ctx, ti, stale, metadataTracking{document: "", fetchedAt: ago(25 * time.Hour), lastError: "", errorAt: nil, errorURL: ""})
	retry := createProjectIssuer(t, ctx, ti, "due-retry", "https://retry.example.com")
	setIssuerMetadataTracking(t, ctx, ti, retry, metadataTracking{document: "", fetchedAt: ago(2 * time.Hour), lastError: "unreadable", errorAt: ago(2 * time.Hour), errorURL: "https://retry.example.com/.well-known/openid-configuration"})
	retryTooSoon := createProjectIssuer(t, ctx, ti, "due-retry-soon", "https://retry-soon.example.com")
	setIssuerMetadataTracking(t, ctx, ti, retryTooSoon, metadataTracking{document: "", fetchedAt: ago(30 * time.Hour), lastError: "unreadable", errorAt: ago(30 * time.Minute), errorURL: "https://retry-soon.example.com/.well-known/openid-configuration"})
	definitive := createProjectIssuer(t, ctx, ti, "due-definitive", "https://definitive.example.com")
	setIssuerMetadataTracking(t, ctx, ti, definitive, metadataTracking{document: "", fetchedAt: nil, lastError: "not found", errorAt: ago(2 * time.Hour), errorURL: ""})
	fresh := createProjectIssuer(t, ctx, ti, "due-fresh", "https://fresh.example.com")
	setIssuerMetadataTracking(t, ctx, ti, fresh, metadataTracking{document: "", fetchedAt: ago(time.Hour), lastError: "", errorAt: nil, errorURL: ""})
	deleted := createProjectIssuer(t, ctx, ti, "due-deleted", "https://deleted.example.com")
	require.NoError(t, ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{ID: deleted.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	global := seedGlobalRemoteIssuer(t, ctx, ti.conn, "due-global")
	org := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, activeOrganizationID(t, ctx), "due-org")

	due := dueIDs(t, ctx, refresher, now)
	for _, id := range []uuid.UUID{neverFetched, stale, retry, global, org} {
		require.Contains(t, due, id)
	}
	for _, id := range []uuid.UUID{retryTooSoon, definitive, fresh, deleted} {
		require.NotContains(t, due, id)
	}

	candidates, _, err := refresher.ListDue(ctx, now, nil, 1000)
	require.NoError(t, err)
	idx := slices.IndexFunc(candidates, func(c remotesessions.IssuerMetadataRefreshCandidate) bool { return c.ID == stale })
	require.GreaterOrEqual(t, idx, 0)
	require.Equal(t, "stale.example.com", candidates[idx].Host)
	require.True(t, candidates[idx].ProjectID.Valid)
	require.Equal(t, activeOrganizationID(t, ctx), candidates[idx].OrganizationID.String)
}

func TestIssuerMetadataRefresh_ListDue_TransientRetryCountsFromTheFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	now := time.Now()
	fetchedAt := now.Add(-30 * time.Hour)
	errorAt := now.Add(-30 * time.Minute)

	id := createProjectIssuer(t, ctx, ti, "due-transient-clock", "https://transient-clock.example.com")
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &fetchedAt, lastError: "unreadable", errorAt: &errorAt, errorURL: "https://transient-clock.example.com/.well-known/openid-configuration"})

	require.NotContains(t, dueIDs(t, ctx, refresher, now), id, "a stale fetch does not make a row due while its last failure is recent")
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(31*time.Minute)), id, "due an hour after the failure")
}

func TestIssuerMetadataRefresh_ListDue_OrdersHealthyStaleAheadOfRecentFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	now := time.Now()
	staleAt := now.Add(-25 * time.Hour)
	olderFetch := now.Add(-48 * time.Hour)
	failedAt := now.Add(-2 * time.Hour)

	healthy := createProjectIssuer(t, ctx, ti, "order-healthy", "https://order-healthy.example.com")
	setIssuerMetadataTracking(t, ctx, ti, healthy, metadataTracking{document: "", fetchedAt: &staleAt, lastError: "", errorAt: nil, errorURL: ""})
	failing := createProjectIssuer(t, ctx, ti, "order-failing", "https://order-failing.example.com")
	setIssuerMetadataTracking(t, ctx, ti, failing, metadataTracking{document: "", fetchedAt: &olderFetch, lastError: "unreadable", errorAt: &failedAt, errorURL: "https://order-failing.example.com/.well-known/openid-configuration"})

	due := dueIDs(t, ctx, refresher, now)
	require.Contains(t, due, healthy)
	require.Contains(t, due, failing)
	require.Less(t, slices.Index(due, healthy), slices.Index(due, failing), "the last visit orders the list, not the last successful fetch")
}

func TestIssuerMetadataRefresh_ListDue_HourlyFailerNeverOutranksHealthyStale(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	now := time.Now()
	staleAt := now.Add(-25 * time.Hour)
	olderFetch := now.Add(-72 * time.Hour)
	lastFailure := now.Add(-61 * time.Minute)

	healthy := createProjectIssuer(t, ctx, ti, "hourly-healthy", "https://hourly-healthy.example.com")
	setIssuerMetadataTracking(t, ctx, ti, healthy, metadataTracking{document: "", fetchedAt: &staleAt, lastError: "", errorAt: nil, errorURL: ""})
	failers := make([]uuid.UUID, 0, 3)
	for i := range 3 {
		slug := "hourly-failer-" + string(rune('a'+i))
		id := createProjectIssuer(t, ctx, ti, slug, "https://"+slug+".example.com")
		setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &olderFetch, lastError: "unreadable", errorAt: &lastFailure, errorURL: "https://" + slug + ".example.com/.well-known/openid-configuration"})
		failers = append(failers, id)
	}

	due := dueIDs(t, ctx, refresher, now)
	require.Contains(t, due, healthy)
	for _, failer := range failers {
		require.Contains(t, due, failer, "an hourly failer is due again after its retry cutoff")
		require.Less(t, slices.Index(due, healthy), slices.Index(due, failer), "a row that failed within the day sorts behind every row not visited in a day")
	}
}

func TestIssuerMetadataRefresh_ListDue_PagesByKeyset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	now := time.Now()

	mine := make([]uuid.UUID, 0, 3)
	for _, slug := range []string{"page-a", "page-b", "page-c"} {
		mine = append(mine, createProjectIssuer(t, ctx, ti, slug, "https://"+slug+".example.com"))
	}

	seen := make([]uuid.UUID, 0)
	var after *remotesessions.IssuerMetadataRefreshCursor
	for range 10000 {
		page, next, err := refresher.ListDue(ctx, now, after, 2)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page), 2)
		for _, candidate := range page {
			require.NotContains(t, seen, candidate.ID, "keyset paging never repeats a row")
			seen = append(seen, candidate.ID)
		}
		if next == nil {
			require.Less(t, len(page), 2, "the cursor ends only on a short page")
			break
		}
		require.Len(t, page, 2)
		require.Equal(t, page[1].ID, next.ID, "the cursor names the last row of the page")
		after = next
	}
	for _, id := range mine {
		require.Contains(t, seen, id)
	}
	require.Equal(t, dueIDs(t, ctx, refresher, now), seen, "walking the pages yields the single-page order")
}

func TestIssuerMetadataRefresh_Reproject_FillsCapabilityColumnsWithoutNetwork(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	now := time.Now()

	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)

	id := createProjectIssuer(t, ctx, ti, "reproject", upstream.URL)
	document, err := json.Marshal(map[string]any{
		"issuer":                                upstream.URL,
		"authorization_endpoint":                upstream.URL + "/authorize",
		"token_endpoint":                        upstream.URL + "/token",
		"jwks_uri":                              upstream.URL + "/jwks",
		"userinfo_endpoint":                     upstream.URL + "/userinfo",
		"scopes_supported":                      []string{"openid"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported":                      []string{"sub", "email"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"backchannel_logout_supported":          true,
	})
	require.NoError(t, err)
	fetchedAt := now.Add(-30 * time.Hour).Truncate(time.Microsecond)
	errorAt := now.Add(-30 * time.Hour).Truncate(time.Microsecond)
	errorURL := upstream.URL + "/.well-known/openid-configuration"
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: string(document), fetchedAt: &fetchedAt, lastError: "old outage", errorAt: &errorAt, errorURL: errorURL})

	before := loadIssuerByID(t, ctx, ti, id)
	require.Nil(t, before.ClaimsSupported, "the stored document predates the capability columns")
	require.False(t, before.BackchannelLogoutSupported.Valid)
	require.Contains(t, reprojectableIDs(t, ctx, refresher, now), id)

	outcome, err := refresher.Reproject(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected, outcome)
	require.Zero(t, requests.Load(), "re-projection never contacts the upstream")

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/userinfo", after.UserinfoEndpoint.String)
	require.Equal(t, []string{"S256"}, after.CodeChallengeMethodsSupported)
	require.Equal(t, []string{"sub", "email"}, after.ClaimsSupported)
	require.Equal(t, []string{"RS256"}, after.IDTokenSigningAlgValuesSupported)
	require.Equal(t, []string{}, after.IntrospectionEndpointAuthMethodsSupported, "an omitted array is captured as empty, not left NULL")
	require.True(t, after.BackchannelLogoutSupported.Valid)
	require.True(t, after.BackchannelLogoutSupported.Bool)
	require.True(t, after.AuthorizationResponseIssParameterSupported.Valid)
	require.False(t, after.AuthorizationResponseIssParameterSupported.Bool)
	require.Equal(t, before.AuthorizationEndpoint, after.AuthorizationEndpoint, "endpoints outside the capability set are not rewritten")
	require.JSONEq(t, string(document), string(after.Metadata), "the stored document is untouched")
	require.Equal(t, fetchedAt, after.MetadataFetchedAt.Time, "re-projection is not a fetch")
	require.Equal(t, "old outage", after.MetadataLastError.String)
	require.Equal(t, errorAt, after.MetadataLastErrorAt.Time)
	require.Equal(t, errorURL, after.MetadataLastErrorUrl.String)
	require.Contains(t, dueIDs(t, ctx, refresher, now), id, "the row is still due for its own network fetch")
	require.NotContains(t, reprojectableIDs(t, ctx, refresher, now), id, "a re-projected row never qualifies again")

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, "system", entry.ActorType)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActorDisplayName, *entry.ActorDisplayName)

	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected])
}

func TestIssuerMetadataRefresh_Reproject_UnvettableDocumentYieldsToNetworkRefreshAfterCutoff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	now := time.Now()

	upstream := fakeIssuerServer(t, nil)
	id := createProjectIssuer(t, ctx, ti, "reproject-invalid", upstream.URL)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{
		document:  `{"issuer":"https://other.example.com","authorization_endpoint":"https://other.example.com/authorize","token_endpoint":"https://other.example.com/token"}`,
		fetchedAt: nil,
		lastError: "",
		errorAt:   nil,
		errorURL:  "",
	})
	require.Contains(t, reprojectableIDs(t, ctx, refresher, now), id)

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	outcome, err := refresher.Reproject(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojectInvalid, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String, "another issuer's endpoints are not adopted")
	require.False(t, after.MetadataFetchedAt.Valid)
	require.Contains(t, after.MetadataLastError.String, "refusing to adopt another authorization server's endpoints")
	require.WithinDuration(t, now, after.MetadataLastErrorAt.Time, time.Minute)
	require.False(t, after.MetadataLastErrorUrl.Valid, "a document that fails vetting is a definitive failure")
	require.NotContains(t, reprojectableIDs(t, ctx, refresher, now), id, "a rejected document is not re-listed every hour")
	require.NotContains(t, reprojectableIDs(t, ctx, refresher, now.Add(25*time.Hour)), id, "the unchanged poison document cannot reset its failure after the stale cutoff")
	require.NotContains(t, dueIDs(t, ctx, refresher, now), id)
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(25*time.Hour)), id, "the network refresh gets a turn after the definitive backoff")

	outcome, err = refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)
	recovered := loadIssuerByID(t, ctx, ti, id)
	require.True(t, recovered.MetadataFetchedAt.Valid)
	require.False(t, recovered.MetadataLastError.Valid)
	require.False(t, recovered.MetadataLastErrorAt.Valid)
	require.False(t, recovered.MetadataLastErrorUrl.Valid)

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, auditsBefore+1, auditsAfter, "only the successful network refresh is audited")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojectInvalid])
}

func TestIssuerMetadataRefresh_Refresh_SuccessStampsFetchedAtAndClearsErrors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)

	id := createProjectIssuer(t, ctx, ti, "refresh-ok", upstream.URL)
	past := time.Now().Add(-30 * time.Hour)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &past, lastError: "previous outage", errorAt: &past, errorURL: upstream.URL + "/.well-known/openid-configuration"})

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/authorize", after.AuthorizationEndpoint.String)
	require.Equal(t, upstream.URL+"/token", after.TokenEndpoint.String)
	require.Equal(t, []string{"S256"}, after.CodeChallengeMethodsSupported)
	require.True(t, after.MetadataFetchedAt.Valid)
	require.WithinDuration(t, time.Now(), after.MetadataFetchedAt.Time, time.Minute)
	require.False(t, after.MetadataLastError.Valid)
	require.False(t, after.MetadataLastErrorAt.Valid)
	require.False(t, after.MetadataLastErrorUrl.Valid)

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, auditsBefore+1, auditsAfter)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, "system", entry.ActorType)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActor, entry.ActorID)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActorDisplayName, *entry.ActorDisplayName)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, activeOrganizationID(t, ctx), entry.OrganizationID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceSystem), *entry.ActingSurface)
	beforeSnapshot, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "https://stale.example.com/authorize", beforeSnapshot["AuthorizationEndpoint"])
	afterSnapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", afterSnapshot["AuthorizationEndpoint"])

	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
	require.NotContains(t, dueIDs(t, ctx, refresher, time.Now()), id)
}

func TestIssuerMetadataRefresh_Refresh_PartialReadKeepsTheRetryURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := partialIssuerServer(t)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-partial", upstream.URL)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshedPartial, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/authorize", after.AuthorizationEndpoint.String, "the readable document is applied")
	require.WithinDuration(t, now, after.MetadataFetchedAt.Time, time.Minute)
	require.Contains(t, after.MetadataLastErrorUrl.String, upstream.URL+"/.well-known/oauth-authorization-server")
	require.WithinDuration(t, now, after.MetadataLastErrorAt.Time, time.Minute)
	require.NotContains(t, dueIDs(t, ctx, refresher, now), id)
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(61*time.Minute)), id, "the unread candidate is retried at the hourly cutoff")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshedPartial])
}

func TestIssuerMetadataRefresh_Refresh_TransientFailureRetriesWithinTheHour(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusServiceUnavailable)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-transient", upstream.URL)
	past := now.Add(-30 * time.Hour).Truncate(time.Microsecond)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &past, lastError: "", errorAt: nil, errorURL: ""})

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.True(t, after.MetadataFetchedAt.Valid)
	require.Equal(t, past, after.MetadataFetchedAt.Time, "a failure never moves metadata_fetched_at")
	require.True(t, after.MetadataLastError.Valid)
	require.Contains(t, after.MetadataLastError.String, "Unexpected HTTP 503")
	require.WithinDuration(t, now, after.MetadataLastErrorAt.Time, time.Minute)
	require.True(t, after.MetadataLastErrorUrl.Valid)
	require.Contains(t, after.MetadataLastErrorUrl.String, upstream.URL+"/.well-known/")
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String, "the stored endpoints stand")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter, "a failed refresh changes nothing worth auditing")

	require.NotContains(t, dueIDs(t, ctx, refresher, now), id, "the failure itself counts as a visit")
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(61*time.Minute)), id, "retried an hour after the failure")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure])
}

func TestIssuerMetadataRefresh_Refresh_RequestTimeoutIsTransient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusRequestTimeout)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-408", upstream.URL)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.True(t, after.MetadataLastErrorUrl.Valid, "a 408 leaves a retry URL behind")
	require.Contains(t, after.MetadataLastErrorUrl.String, upstream.URL+"/.well-known/")
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(61*time.Minute)), id, "retried an hour later, not a day later")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure])
}

func TestIssuerMetadataRefresh_Refresh_NewerFetchDuringDiscoveryIsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var id uuid.UUID
	var armed atomic.Bool
	var once sync.Once
	operatorStamp := time.Now().Truncate(time.Microsecond)
	operatorDocument := `{"issuer":"https://operator.example.com","authorization_endpoint":"https://operator.example.com/authorize","token_endpoint":"https://operator.example.com/token"}`
	upstream := fakeIssuerServer(t, func(map[string]any) {
		// Runs while the sweep is mid-discovery: an operator refresh lands a newer document on the row.
		if !armed.Load() {
			return
		}
		once.Do(func() {
			setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: operatorDocument, fetchedAt: &operatorStamp, lastError: "", errorAt: nil, errorURL: ""})
		})
	})
	id = createProjectIssuer(t, ctx, ti, "refresh-lost-update", upstream.URL)
	armed.Store(true)

	auditsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.JSONEq(t, operatorDocument, string(after.Metadata), "the newer document stands")
	require.Equal(t, operatorStamp, after.MetadataFetchedAt.Time, "the newer fetch stamp stands")
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String, "the sweep wrote nothing")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict])
}

func TestIssuerMetadataRefresh_Refresh_FailureAfterNewerFetchIsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var id uuid.UUID
	var armed atomic.Bool
	var once sync.Once
	operatorStamp := time.Now().Truncate(time.Microsecond)
	operatorDocument := `{"issuer":"https://operator.example.com","authorization_endpoint":"https://operator.example.com/authorize","token_endpoint":"https://operator.example.com/token"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if armed.Load() {
			once.Do(func() {
				setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: operatorDocument, fetchedAt: &operatorStamp})
			})
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	id = createProjectIssuer(t, ctx, ti, "refresh-stale-failure", upstream.URL)
	armed.Store(true)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.JSONEq(t, operatorDocument, string(after.Metadata))
	require.Equal(t, operatorStamp, after.MetadataFetchedAt.Time)
	require.False(t, after.MetadataLastError.Valid, "the stale failure cannot reintroduce an error")
	require.False(t, after.MetadataLastErrorAt.Valid)
	require.False(t, after.MetadataLastErrorUrl.Valid)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict])
}

func TestIssuerMetadataRefresh_Refresh_DefinitiveFailureWaitsForTheDailyCutoff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusNotFound)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-definitive", upstream.URL)
	past := now.Add(-2 * time.Hour).Truncate(time.Microsecond)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &past, lastError: "", errorAt: nil, errorURL: ""})

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, past, after.MetadataFetchedAt.Time)
	require.True(t, after.MetadataLastError.Valid)
	require.Contains(t, after.MetadataLastError.String, "OAuth metadata not found")
	require.True(t, after.MetadataLastErrorAt.Valid)
	require.False(t, after.MetadataLastErrorUrl.Valid, "a definitive failure records no retry URL")

	require.NotContains(t, dueIDs(t, ctx, refresher, now.Add(23*time.Hour)), id, "not due again until a day after the failure")
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(25*time.Hour)), id)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure])
}

func TestIssuerMetadataRefresh_Refresh_DefinitiveFailureOnNeverFetchedRowWaitsForTheDailyCutoff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusNotFound)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-never-definitive", upstream.URL)
	require.Contains(t, dueIDs(t, ctx, refresher, now), id)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.False(t, after.MetadataFetchedAt.Valid)
	require.WithinDuration(t, now, after.MetadataLastErrorAt.Time, time.Minute)
	require.False(t, after.MetadataLastErrorUrl.Valid)

	require.NotContains(t, dueIDs(t, ctx, refresher, now), id, "a never-fetched row with a definitive answer is not retried hourly")
	require.NotContains(t, dueIDs(t, ctx, refresher, now.Add(23*time.Hour)), id)
	require.Contains(t, dueIDs(t, ctx, refresher, now.Add(25*time.Hour)), id)
}

func TestIssuerMetadataRefresh_Refresh_UntrustedDocumentIsDefinitive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		doc["issuer"] = "https://somebody-else.example.com"
	})

	id := createProjectIssuer(t, ctx, ti, "refresh-untrusted", upstream.URL)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Contains(t, after.MetadataLastError.String, "refusing to adopt another authorization server's endpoints")
	require.False(t, after.MetadataLastErrorUrl.Valid)
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String)
}

func TestIssuerMetadataRefresh_Refresh_OrgLevelRowAuditsAsSystem(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)
	orgID := activeOrganizationID(t, ctx)

	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, conv.ToPGText(orgID), "refresh-org-level", upstream.URL)
	candidate := refreshCandidate(t, ctx, ti, id)
	require.False(t, candidate.ProjectID.Valid)
	require.Equal(t, orgID, candidate.OrganizationID.String)

	outcome, err := refresher.Refresh(ctx, candidate)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/register", after.RegistrationEndpoint.String)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, orgID, entry.OrganizationID)
	require.False(t, entry.ProjectID.Valid, "an organization-level row audits to the org feed with no project")
	require.Equal(t, "system", entry.ActorType)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActor, entry.ActorID)
	require.NotNil(t, entry.ActorDisplayName)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActorDisplayName, *entry.ActorDisplayName)
}

func TestIssuerMetadataRefresh_Refresh_ProjectRowWithoutOrgAuditsUnderTheProjectOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: projectID, Valid: true}, pgtype.Text{String: "", Valid: false}, "refresh-legacy-project", upstream.URL)
	candidate := refreshCandidate(t, ctx, ti, id)
	require.True(t, candidate.ProjectID.Valid)
	require.False(t, candidate.OrganizationID.Valid, "a legacy project row carries no organization of its own")

	outcome, err := refresher.Refresh(ctx, candidate)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/register", after.RegistrationEndpoint.String)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, entry.OrganizationID, "the organization is resolved through the project")
	require.True(t, entry.ProjectID.Valid)
	require.Equal(t, projectID, entry.ProjectID.UUID)
	require.Equal(t, "system", entry.ActorType)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActor, entry.ActorID)
}

func TestIssuerMetadataRefresh_Refresh_GlobalRowIsRefreshedWithoutAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)

	id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, pgtype.Text{String: "", Valid: false}, "refresh-global", upstream.URL)
	require.Contains(t, dueIDs(t, ctx, refresher, time.Now()), id)

	auditsBefore, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/register", after.RegistrationEndpoint.String)
	require.True(t, after.MetadataFetchedAt.Valid)

	auditsAfter, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter, "a global row belongs to no organization feed")
}

func TestIssuerMetadataRefresh_Refresh_MissingRowIsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	outcome, err := refresher.Refresh(ctx, remotesessions.IssuerMetadataRefreshCandidate{
		ID:                  uuid.New(),
		IssuerURL:           "https://missing.example.com",
		Host:                "missing.example.com",
		ProjectID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:      pgtype.Text{String: "", Valid: false},
		TunneledMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, outcome)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict])
}

func TestIssuerMetadataRefresh_Refresh_RescopedRowIsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)

	id := createProjectIssuer(t, ctx, ti, "refresh-rescoped", upstream.URL)
	candidate := refreshCandidate(t, ctx, ti, id)
	candidate.ProjectID = uuid.NullUUID{UUID: uuid.New(), Valid: true}

	outcome, err := refresher.Refresh(ctx, candidate)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, outcome, "a row listed under another scope is never read across tenants")

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String)
	require.False(t, after.MetadataFetchedAt.Valid)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict])
}

func TestIssuerMetadataRefreshHost(t *testing.T) {
	t.Parallel()

	require.Equal(t, "idp.example.com:8443", remotesessions.IssuerMetadataRefreshHost("https://IdP.Example.com:8443/tenant-a"))
	require.Equal(t, "idp.example.com", remotesessions.IssuerMetadataRefreshHost("https://IdP.Example.com:443/tenant-a"), "a default port is one host with the bare name")
	require.Equal(t, "idp.example.com", remotesessions.IssuerMetadataRefreshHost("https://idp.example.com/tenant-b"))
	require.Equal(t, "localhost", remotesessions.IssuerMetadataRefreshHost("http://localhost:80"))
	require.Equal(t, "localhost:443", remotesessions.IssuerMetadataRefreshHost("http://localhost:443"), "443 is only default for https")
	require.Equal(t, "[::1]", remotesessions.IssuerMetadataRefreshHost("https://[::1]:443/x"))
	require.Empty(t, remotesessions.IssuerMetadataRefreshHost("not a url"))
	require.Empty(t, remotesessions.IssuerMetadataRefreshHost("idp.example.com/no-scheme"))
	require.Empty(t, remotesessions.IssuerMetadataRefreshHost("https://"))
	require.Empty(t, remotesessions.IssuerMetadataRefreshHost(""))
}
