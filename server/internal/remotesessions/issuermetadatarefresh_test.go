package remotesessions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// refreshCandidate builds the refresher's view of a stored row.
func refreshCandidate(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) remotesessions.IssuerMetadataRefreshCandidate {
	t.Helper()
	row := loadIssuerByID(t, ctx, ti, id)
	return remotesessions.IssuerMetadataRefreshCandidate{
		ID:             row.ID,
		IssuerURL:      row.Issuer,
		ProjectID:      row.ProjectID,
		OrganizationID: row.OrganizationID,
	}
}

func createProjectIssuer(t *testing.T, ctx context.Context, ti *testInstance, slug, issuerURL string) uuid.UUID {
	t.Helper()
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL(slug, issuerURL))
	require.NoError(t, err)
	return uuid.MustParse(created.ID)
}

// due reports what a flow-time use of the stored row would do now.
func due(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, now time.Time) (reproject, fetch bool) {
	t.Helper()
	return remotesessions.PlanIssuerMetadataRefresh(remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)), now)
}

func fetchDue(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, now time.Time) bool {
	t.Helper()
	_, fetch := due(t, ctx, ti, id, now)
	return fetch
}

func reprojectDue(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, now time.Time) bool {
	t.Helper()
	reproject, _ := due(t, ctx, ti, id, now)
	return reproject
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
	require.True(t, reprojectDue(t, ctx, ti, id, now))

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
	require.True(t, fetchDue(t, ctx, ti, id, now), "the row is still due for its own network fetch")
	require.False(t, reprojectDue(t, ctx, ti, id, now), "a re-projected row never qualifies again")

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
	require.True(t, reprojectDue(t, ctx, ti, id, now))

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
	require.False(t, reprojectDue(t, ctx, ti, id, now), "a rejected document is not re-projected again")
	require.False(t, reprojectDue(t, ctx, ti, id, now.Add(25*time.Hour)), "the unchanged poison document cannot reset its failure after the stale cutoff")
	require.False(t, fetchDue(t, ctx, ti, id, now))
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(25*time.Hour)), "the network refresh gets a turn after the definitive backoff")

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
	require.False(t, fetchDue(t, ctx, ti, id, time.Now()))
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
	require.False(t, fetchDue(t, ctx, ti, id, now))
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(61*time.Minute)), "the unread candidate is retried after the retry window")
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

	require.False(t, fetchDue(t, ctx, ti, id, now), "the failure itself counts as a visit")
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(61*time.Minute)), "retried on use an hour after the failure")
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
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(61*time.Minute)), "retried an hour later, not a day later")
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
		// Runs while the refresh is mid-discovery: an operator refresh lands a newer document on the row.
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
	require.Equal(t, "https://stale.example.com/authorize", after.AuthorizationEndpoint.String, "the refresh wrote nothing")

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

// Two replicas failing one issuer: the failure that lands second cannot replace the first's classification.
func TestIssuerMetadataRefresh_Refresh_FailureAfterConcurrentFailureIsConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var id uuid.UUID
	var armed atomic.Bool
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if armed.Load() {
			once.Do(func() {
				// Another replica records a definitive failure while this one is mid-discovery.
				setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: nil, lastError: "OAuth metadata not found elsewhere", errorAt: nil, errorURL: ""})
			})
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)

	id = createProjectIssuer(t, ctx, ti, "refresh-concurrent-failure", upstream.URL)
	armed.Store(true)

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeConflict, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, "OAuth metadata not found elsewhere", after.MetadataLastError.String, "the concurrent definitive failure stands")
	require.False(t, after.MetadataLastErrorUrl.Valid, "the transient retry URL never lands over it")
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

	require.False(t, fetchDue(t, ctx, ti, id, now.Add(23*time.Hour)), "not due again until a day after the failure")
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(25*time.Hour)))
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure])
}

func TestIssuerMetadataRefresh_Refresh_DefinitiveFailureOnNeverFetchedRowWaitsForTheDailyCutoff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusNotFound)
	now := time.Now()

	id := createProjectIssuer(t, ctx, ti, "refresh-never-definitive", upstream.URL)
	require.True(t, fetchDue(t, ctx, ti, id, now))

	outcome, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, id))
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeDefinitiveFailure, outcome)

	after := loadIssuerByID(t, ctx, ti, id)
	require.False(t, after.MetadataFetchedAt.Valid)
	require.WithinDuration(t, now, after.MetadataLastErrorAt.Time, time.Minute)
	require.False(t, after.MetadataLastErrorUrl.Valid)

	require.False(t, fetchDue(t, ctx, ti, id, now), "a never-fetched row with a definitive answer is not retried within the day")
	require.False(t, fetchDue(t, ctx, ti, id, now.Add(23*time.Hour)))
	require.True(t, fetchDue(t, ctx, ti, id, now.Add(25*time.Hour)))
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
	require.True(t, fetchDue(t, ctx, ti, id, time.Now()))

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
		ID:             uuid.New(),
		IssuerURL:      "https://missing.example.com",
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID: pgtype.Text{String: "", Valid: false},
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

func TestIssuerMetadataRefresh_NoteUse_FreshRowDoesNothing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	id := createProjectIssuer(t, ctx, ti, "note-use-fresh", upstream.URL)
	recent := time.Now().Add(-time.Hour)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: "", fetchedAt: &recent, lastError: "", errorAt: nil, errorURL: ""})

	refresher.NoteUse(ctx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	refresher.Wait()

	require.Zero(t, requests.Load(), "a row fetched within the day is left alone")
	require.Empty(t, outcomeCounts(t, reader))
}

func TestIssuerMetadataRefresh_NoteUse_RefreshesOffTheRequestPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)
	id := createProjectIssuer(t, ctx, ti, "note-use-stale", upstream.URL)

	// The request context is already done: the refresh must not inherit its cancellation.
	requestCtx, cancel := context.WithCancel(ctx)
	cancel()
	refresher.NoteUse(requestCtx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	refresher.Wait()

	after := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, upstream.URL+"/authorize", after.AuthorizationEndpoint.String)
	require.True(t, after.MetadataFetchedAt.Valid)
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, "system", entry.ActorType)
	require.Equal(t, remotesessions.IssuerMetadataRefreshActor, entry.ActorID)

	require.False(t, fetchDue(t, ctx, ti, id, time.Now()), "the next use finds the row fresh")
}

func TestIssuerMetadataRefresh_NoteUse_ReprojectsThenFetchesInOneVisit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)
	id := createProjectIssuer(t, ctx, ti, "note-use-both", upstream.URL)
	document, err := json.Marshal(map[string]any{
		"issuer":                 upstream.URL,
		"authorization_endpoint": upstream.URL + "/authorize",
		"token_endpoint":         upstream.URL + "/token",
		"claims_supported":       []string{"sub"},
	})
	require.NoError(t, err)
	past := time.Now().Add(-30 * time.Hour)
	setIssuerMetadataTracking(t, ctx, ti, id, metadataTracking{document: string(document), fetchedAt: &past, lastError: "", errorAt: nil, errorURL: ""})
	reproject, fetch := due(t, ctx, ti, id, time.Now())
	require.True(t, reproject)
	require.True(t, fetch)

	refresher.NoteUse(ctx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	refresher.Wait()

	counts := outcomeCounts(t, reader)
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeReprojected])
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
	after := loadIssuerByID(t, ctx, ti, id)
	require.WithinDuration(t, time.Now(), after.MetadataFetchedAt.Time, time.Minute)
	require.Equal(t, []string{"S256"}, after.CodeChallengeMethodsSupported, "the fetched document wins over the re-projected one")
	reproject, fetch = due(t, ctx, ti, id, time.Now())
	require.False(t, reproject)
	require.False(t, fetch)
}

func TestIssuerMetadataRefresh_NoteUse_ConcurrentUsesRefreshOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	id := createProjectIssuer(t, ctx, ti, "note-use-burst", upstream.URL)
	use := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id))

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { refresher.NoteUse(ctx, use) })
	}
	wg.Wait()
	refresher.Wait()

	counts := outcomeCounts(t, reader)
	require.Equal(t, int64(1), counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
	require.LessOrEqual(t, counts[remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedInFlight], int64(15), "callers that catch the refresh in flight are skipped; the rest replan from the refreshed row and fetch nothing")
	require.True(t, loadIssuerByID(t, ctx, ti, id).MetadataFetchedAt.Valid)
	require.LessOrEqual(t, requests.Load(), int32(2), "one discovery run probes at most the two well-known locations")
}

func TestIssuerMetadataRefresh_NoteUse_ReplansStaleSnapshotAfterSuccessfulVisit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var requests atomic.Int32
	upstream := fakeIssuerServer(t, func(map[string]any) { requests.Add(1) })
	id := createProjectIssuer(t, ctx, ti, "note-use-stale-success", upstream.URL)
	staleUse := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id))

	refresher.NoteUse(ctx, staleUse)
	refresher.Wait()
	requestsAfterRefresh := requests.Load()
	require.Positive(t, requestsAfterRefresh)

	refresher.NoteUse(ctx, staleUse)
	refresher.Wait()

	require.Equal(t, requestsAfterRefresh, requests.Load(), "the stale flow snapshot is replanned from the refreshed row")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed])
}

func TestIssuerMetadataRefresh_NoteUse_ReplansStaleSnapshotDuringFailureBackoff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	id := createProjectIssuer(t, ctx, ti, "note-use-stale-failure", upstream.URL)
	staleUse := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id))

	refresher.NoteUse(ctx, staleUse)
	refresher.Wait()
	requestsAfterFailure := requests.Load()
	require.Positive(t, requestsAfterFailure)

	refresher.NoteUse(ctx, staleUse)
	refresher.Wait()

	require.Equal(t, requestsAfterFailure, requests.Load(), "the stale flow snapshot is replanned from the failure stamp")
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure])
}

func TestIssuerMetadataRefresh_NoteUse_ListClientsRefreshesTheIssuerItRenders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)

	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, conv.ToPGText(authCtx.ActiveOrganizationID), "note-use-consent", upstream.URL)
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "note-use-consent-usi")
	seedRemoteClientAtTier(t, ctx, ti.conn, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, conv.ToPGText(authCtx.ActiveOrganizationID), issuerID, "note-use-consent-b", userIssuer)
	seedRemoteClientAtTier(t, ctx, ti.conn, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, conv.ToPGText(authCtx.ActiveOrganizationID), issuerID, "note-use-consent-c", userIssuer)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		testenv.NewEncryptionClient(t),
		policy,
		ti.redisCache,
		mustURL(t, "http://localhost"),
		remotesessions.WithIssuerMetadataRefresher(refresher),
	)

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 2)
	require.Equal(t, upstream.URL+"/authorize", clients[0].AuthorizationEndpoint, "the render itself uses the stored row")
	refresher.Wait()

	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed], "two clients on one issuer refresh it once")
	after := loadIssuerByID(t, ctx, ti, issuerID)
	require.True(t, after.MetadataFetchedAt.Valid)
	require.Equal(t, upstream.URL+"/register", after.RegistrationEndpoint.String)
}

func TestIssuerMetadataRefresh_NoteUse_AuditsAsSystemEvenWhenAnAgentTriggersIt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	upstream := fakeIssuerServer(t, nil)
	id := createProjectIssuer(t, ctx, ti, "note-use-agent", upstream.URL)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentCtx := contextvalues.WithAuthenticatedActor(ctx, authCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	agentCtx = contextvalues.SetOAuthClientID(agentCtx, "agent-oauth-client")

	refresher.NoteUse(agentCtx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	refresher.Wait()

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, id.String(), entry.SubjectID)
	require.Equal(t, "system", entry.ActorType, "the request's agent actor never reaches the entry")
	require.Equal(t, remotesessions.IssuerMetadataRefreshActor, entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceSystem), *entry.ActingSurface)
	require.Nil(t, entry.ActingClientID, "the request's OAuth client never reaches the entry")
}

func TestIssuerMetadataRefresh_NoteUse_FailureStampPacesTheRetry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)
	upstream := statusServer(t, http.StatusServiceUnavailable)
	id := createProjectIssuer(t, ctx, ti, "note-use-paced", upstream.URL)

	refresher.NoteUse(ctx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	refresher.Wait()
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure])

	require.False(t, fetchDue(t, ctx, ti, id, time.Now()), "the failure counts as a visit")
	require.True(t, fetchDue(t, ctx, ti, id, time.Now().Add(61*time.Minute)))
}

func TestIssuerMetadataRefresh_NoteUse_SkipsWhenEverySlotIsBusy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	refresher, reader := newIssuerMetadataRefresher(t, ti)

	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() { close(release) })

	for i := range 5 {
		id := createProjectIssuer(t, ctx, ti, fmt.Sprintf("note-use-busy-%d", i), upstream.URL+"/"+strconv.Itoa(i))
		refresher.NoteUse(ctx, remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, id)))
	}
	require.Equal(t, int64(1), outcomeCounts(t, reader)[remotesessionmetrics.IssuerMetadataRefreshOutcomeSkippedBusy], "four slots absorb four issuers; the fifth is skipped on the request path")
}

func TestIssuerMetadataRefresh_FlowRowReprojectionFlagMatchesTheStoredRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	upstream := fakeIssuerServer(t, nil)

	issuerID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}, conv.ToPGText(authCtx.ActiveOrganizationID), "flow-flag", upstream.URL)
	clientRowID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *authCtx.ProjectID, issuerID, "flow-flag-client")
	document := `{"issuer":"` + upstream.URL + `","authorization_endpoint":"` + upstream.URL + `/authorize","token_endpoint":"` + upstream.URL + `/token"}`

	for _, tracking := range []metadataTracking{
		{document: "", fetchedAt: nil, lastError: "", errorAt: nil, errorURL: ""},
		{document: document, fetchedAt: nil, lastError: "", errorAt: nil, errorURL: ""},
	} {
		setIssuerMetadataTracking(t, ctx, ti, issuerID, tracking)
		flow, err := repo.New(ti.conn).GetRemoteSessionClientWithIssuerByID(ctx, clientRowID)
		require.NoError(t, err)
		fromRow := remotesessions.IssuerMetadataUseFromRow(loadIssuerByID(t, ctx, ti, issuerID))
		require.Equal(t, fromRow.NeedsReprojection, flow.MetadataNeedsReprojection, "document present: %t", tracking.document != "")
		require.Equal(t, tracking.document != "", flow.MetadataNeedsReprojection, "seeded rows have NULL capability columns")
		require.Equal(t, fromRow.ProjectID, flow.IssuerProjectID)
		require.Equal(t, fromRow.OrganizationID, flow.IssuerOrganizationID)
	}

	refresher, _ := newIssuerMetadataRefresher(t, ti)
	_, err := refresher.Refresh(ctx, refreshCandidate(t, ctx, ti, issuerID))
	require.NoError(t, err)
	flow, err := repo.New(ti.conn).GetRemoteSessionClientWithIssuerByID(ctx, clientRowID)
	require.NoError(t, err)
	require.False(t, flow.MetadataNeedsReprojection, "a refreshed row has every capability column set")
	require.True(t, flow.MetadataFetchedAt.Valid)
}
