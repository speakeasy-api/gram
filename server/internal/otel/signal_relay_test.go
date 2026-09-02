package otel

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
)

func TestSignalRelayDestinationUsesConfiguredSignalEndpoint(t *testing.T) {
	t.Parallel()

	requestPath := ""
	requestContentType := ""
	requestHeader := ""
	var requestReadErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.Path
		requestContentType = request.Header.Get("Content-Type")
		requestHeader = request.Header.Get("Authorization")
		_, requestReadErr = io.Copy(io.Discard, request.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := newSignalRelay(nil, nil, policy, "/v1/metrics", "metric")
	destination, err := relay.newDestination(
		relayTestRouteKey("organization-id", testLogProjectID),
		server.URL,
		map[string]string{"Authorization": "Bearer test-token"},
		true,
	)
	require.NoError(t, err)

	err = destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})

	require.NoError(t, err)
	require.NoError(t, requestReadErr)
	require.Equal(t, "/v1/metrics", requestPath)
	require.Equal(t, "application/x-protobuf", requestContentType)
	require.Equal(t, "Bearer test-token", requestHeader)
}

func TestSignalRelayLoadsActiveDataExportRoute(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	headers := encryptRelayTestHeaders(t, enc, map[string]string{"Authorization": "Bearer route"})
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test/otlp", headers, "include")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, "https://collector.example.test/otlp/v1/traces", loaded.endpoint)
	require.Equal(t, "Bearer route", loaded.headers.Get("Authorization"))
	require.True(t, loaded.includeSensitiveData)
	require.Equal(t, projectID, loaded.projectID)
}
func TestSignalRelayReloadsRoutesThatMayExportSensitiveData(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay.now = func() time.Time { return now }
	projectID := createRelayTestProject(t, db, "org-test")
	headers := encryptRelayTestHeaders(t, enc, nil)
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test/otlp", headers, "include")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	key := relayRouteKey{organizationID: "org-test", projectID: projectID}

	included, err := relay.destinationForRoute(t.Context(), key)
	require.NoError(t, err)
	require.True(t, included.includeSensitiveData)
	require.NotContains(t, relay.destinationCache, key)

	_, err = dataexportsrepo.New(db).UpdateOtelDestination(t.Context(), dataexportsrepo.UpdateOtelDestinationParams{
		Name:             destination.Name,
		EndpointUrl:      destination.EndpointUrl,
		HeadersEncrypted: destination.HeadersEncrypted,
		SensitiveData:    pgtype.Text{String: "exclude", Valid: true},
		OrganizationID:   destination.OrganizationID,
		ProjectID:        destination.ProjectID,
		ID:               destination.ID,
	})
	require.NoError(t, err)

	excluded, err := relay.destinationForRoute(t.Context(), key)
	require.NoError(t, err)
	require.False(t, excluded.includeSensitiveData)
	require.Contains(t, relay.destinationCache, key)
}

func TestSignalRelayTreatsUnknownSensitiveDataPolicyAsExclude(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "unknown")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.False(t, loaded.includeSensitiveData)
}

func TestSignalRelayRoutesSameOrganizationProjectsIndependently(t *testing.T) {
	t.Parallel()

	firstRequests := make(chan string, 1)
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		firstRequests <- request.URL.Path + "|" + request.Header.Get("X-Project")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(firstServer.Close)
	secondRequests := make(chan string, 1)
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		secondRequests <- request.URL.Path + "|" + request.Header.Get("X-Project")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(secondServer.Close)

	db, enc, relay := newRelayRouteTest(t, "/v1/logs")
	firstProjectID := createRelayTestProject(t, db, "org-test")
	secondProjectID := createRelayTestProject(t, db, "org-test")
	firstDestination := createRelayTestDestination(
		t,
		db,
		"org-test",
		firstProjectID,
		firstServer.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Project": "first"}),
		"exclude",
	)
	secondDestination := createRelayTestDestination(
		t,
		db,
		"org-test",
		secondProjectID,
		secondServer.URL,
		encryptRelayTestHeaders(t, enc, map[string]string{"X-Project": "second"}),
		"include",
	)
	createRelayTestRoute(t, db, "org-test", firstProjectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: firstDestination.ID, Valid: true})
	createRelayTestRoute(t, db, "org-test", secondProjectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: secondDestination.ID, Valid: true})

	first, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: firstProjectID})
	require.NoError(t, err)
	second, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: secondProjectID})
	require.NoError(t, err)
	require.False(t, first.includeSensitiveData)
	require.True(t, second.includeSensitiveData)
	require.NoError(t, first.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.NoError(t, second.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.Equal(t, "/v1/logs|first", <-firstRequests)
	require.Equal(t, "/v1/logs|second", <-secondRequests)
}

func TestSignalRelayReturnsNoDestinationWithoutRoute(t *testing.T) {
	t.Parallel()

	db, _, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")

	destination, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, destination)
}

func TestSignalRelayReturnsNoDestinationForDisabledRoute(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "exclude")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, false, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestSignalRelayReturnsNoDestinationWithoutSelectedOtelDestination(t *testing.T) {
	t.Parallel()

	db, _, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	destination, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, destination)
}

func TestSignalRelayReturnsNoDestinationForSoftDeletedRoute(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "exclude")
	route := createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	_, err := dataexportsrepo.New(db).SoftDeleteDataExportRoute(t.Context(), dataexportsrepo.SoftDeleteDataExportRouteParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		ID:             route.ID,
	})
	require.NoError(t, err)

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestSignalRelayReturnsNoDestinationForSoftDeletedDestination(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "exclude")
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})
	_, err := dataexportsrepo.New(db).SoftDeleteOtelDestination(t.Context(), dataexportsrepo.SoftDeleteOtelDestinationParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		ID:             destination.ID,
	})
	require.NoError(t, err)

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestSignalRelayIgnoresRoutesForOtherDataSources(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", projectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "exclude")
	createRelayTestRoute(t, db, "org-test", projectID, "risk_findings", true, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestSignalRelayCannotResolveAnotherProjectRoute(t *testing.T) {
	t.Parallel()

	db, enc, relay := newRelayRouteTest(t, "/v1/traces")
	configuredProjectID := createRelayTestProject(t, db, "org-test")
	otherProjectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(t, db, "org-test", configuredProjectID, "https://collector.example.test", encryptRelayTestHeaders(t, enc, nil), "exclude")
	createRelayTestRoute(t, db, "org-test", configuredProjectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: otherProjectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
	loaded, err = relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "other-org", projectID: configuredProjectID})
	require.NoError(t, err)
	require.Nil(t, loaded)
}

func TestSignalRelayFailsMalformedEncryptedHeaders(t *testing.T) {
	t.Parallel()

	db, _, relay := newRelayRouteTest(t, "/v1/traces")
	projectID := createRelayTestProject(t, db, "org-test")
	destination := createRelayTestDestination(
		t,
		db,
		"org-test",
		projectID,
		"https://collector.example.test",
		pgtype.Text{String: "not-valid-ciphertext", Valid: true},
		"exclude",
	)
	createRelayTestRoute(t, db, "org-test", projectID, relayDataSourceProductTelemetry, true, uuid.NullUUID{UUID: destination.ID, Valid: true})

	loaded, err := relay.destinationForRoute(t.Context(), relayRouteKey{organizationID: "org-test", projectID: projectID})
	require.ErrorContains(t, err, "decrypt destination headers")
	require.Nil(t, loaded)
}

func TestSignalRelayDestinationDrainsFailedResponses(t *testing.T) {
	t.Parallel()

	var newConnections atomic.Int64
	errorBody := strings.Repeat("x", maxRelayErrorBodyBytes+1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, errorBody)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConnections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	relay := newSignalRelay(nil, nil, policy, "/v1/traces", "trace")
	destination, err := relay.newDestination(relayTestRouteKey("organization-id", testLogProjectID), server.URL, nil, true)
	require.NoError(t, err)

	require.Error(t, destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.Error(t, destination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{}))
	require.Equal(t, int64(1), newConnections.Load())
}

func TestSignalRelayDestinationSanitizesResponseDiagnostics(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/permanent":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "line one\nline two\x07")
		case "/retryable":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "provider-secret-marker")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	permanentRelay := newSignalRelay(nil, nil, policy, "/permanent", "trace")
	permanentDestination, err := permanentRelay.newDestination(relayTestRouteKey("organization-id", testLogProjectID), server.URL, nil, true)
	require.NoError(t, err)

	err = permanentDestination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "line one line two ")
	require.NotContains(t, err.Error(), "\n")
	require.NotContains(t, err.Error(), "\x07")

	retryableRelay := newSignalRelay(nil, nil, policy, "/retryable", "trace")
	retryableDestination, err := retryableRelay.newDestination(relayTestRouteKey("organization-id", testLogProjectID), server.URL, nil, true)
	require.NoError(t, err)

	err = retryableDestination.export(t.Context(), &collectortracev1.ExportTraceServiceRequest{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "provider-secret-marker")
}

func TestSignalRelayCacheReturnsActiveAndRemovesExpiredDestinations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	active, activeTransport := newTrackedRelayDestination()
	expired, expiredTransport := newTrackedRelayDestination()
	activeKey := relayTestRouteKey("active", testLogProjectID)
	expiredKey := relayTestRouteKey("expired", testLogProjectID)
	relay.destinationCache[activeKey] = cachedRelayDestination{
		destination: active,
		expiresAt:   now.Add(time.Minute),
	}
	relay.destinationCache[expiredKey] = cachedRelayDestination{
		destination: expired,
		expiresAt:   now,
	}

	got, ok := relay.cachedDestination(activeKey, now)
	require.True(t, ok)
	require.Same(t, active, got)
	require.Zero(t, activeTransport.closeCalls.Load())

	got, ok = relay.cachedDestination(expiredKey, now)
	require.False(t, ok)
	require.Nil(t, got)
	require.NotContains(t, relay.destinationCache, expiredKey)
	require.Equal(t, int64(1), expiredTransport.closeCalls.Load())
}

func TestSignalRelayCacheIsolatesProjectsWithinOrganization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	first, _ := newTrackedRelayDestination()
	second, _ := newTrackedRelayDestination()
	firstKey := relayTestRouteKey("organization-id", testLogProjectID)
	secondKey := relayTestRouteKey("organization-id", testLogOtherProjectID)
	relay.destinationCache[firstKey] = cachedRelayDestination{
		destination: first,
		expiresAt:   now.Add(time.Minute),
	}
	relay.destinationCache[secondKey] = cachedRelayDestination{
		destination: second,
		expiresAt:   now.Add(time.Minute),
	}

	firstResult, ok := relay.cachedDestination(firstKey, now)
	require.True(t, ok)
	require.Same(t, first, firstResult)
	secondResult, ok := relay.cachedDestination(secondKey, now)
	require.True(t, ok)
	require.Same(t, second, secondResult)
}

func TestSignalRelayCacheInsertionPrunesExpiredDestinations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	expired, expiredTransport := newTrackedRelayDestination()
	expiredKey := relayTestRouteKey("expired", testLogProjectID)
	newKey := relayTestRouteKey("new", testLogProjectID)
	relay.destinationCache[expiredKey] = cachedRelayDestination{
		destination: expired,
		expiresAt:   now,
	}

	relay.cacheDestination(newKey, cachedRelayDestination{
		destination: nil,
		expiresAt:   now.Add(time.Minute),
	}, now)

	require.NotContains(t, relay.destinationCache, expiredKey)
	require.Contains(t, relay.destinationCache, newKey)
	require.Equal(t, int64(1), expiredTransport.closeCalls.Load())
}

func TestSignalRelayCacheReplacementClosesOldDestination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	oldDestination, oldTransport := newTrackedRelayDestination()
	newDestination, newTransport := newTrackedRelayDestination()
	key := relayTestRouteKey("organization-id", testLogProjectID)
	relay.cacheDestination(key, cachedRelayDestination{
		destination: oldDestination,
		expiresAt:   now.Add(time.Minute),
	}, now)

	relay.cacheDestination(key, cachedRelayDestination{
		destination: newDestination,
		expiresAt:   now.Add(2 * time.Minute),
	}, now)

	got, ok := relay.cachedDestination(key, now)
	require.True(t, ok)
	require.Same(t, newDestination, got)
	require.Equal(t, int64(1), oldTransport.closeCalls.Load())
	require.Zero(t, newTransport.closeCalls.Load())
}

func TestSignalRelayCacheEvictsEarliestExpiryAtCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	relay := newSignalRelay(nil, nil, nil, "", "trace")
	oldestDestination, oldestTransport := newTrackedRelayDestination()
	oldestKey := relayTestRouteKey("organization-0000", testLogProjectID)
	for i := range relayDestinationCacheMaxEntries {
		destination := (*relayDestination)(nil)
		if i == 0 {
			destination = oldestDestination
		}
		key := relayTestRouteKey(fmt.Sprintf("organization-%04d", i), testLogProjectID)
		relay.destinationCache[key] = cachedRelayDestination{
			destination: destination,
			expiresAt:   now.Add(time.Duration(i+1) * time.Second),
		}
	}

	newKey := relayTestRouteKey("new-organization", testLogProjectID)
	relay.cacheDestination(newKey, cachedRelayDestination{
		destination: nil,
		expiresAt:   now.Add(time.Hour),
	}, now)

	require.Len(t, relay.destinationCache, relayDestinationCacheMaxEntries)
	require.NotContains(t, relay.destinationCache, oldestKey)
	require.Contains(t, relay.destinationCache, newKey)
	require.Equal(t, int64(1), oldestTransport.closeCalls.Load())
}

type closeIdleTrackingRoundTripper struct {
	closeCalls atomic.Int64
}

func (t *closeIdleTrackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unexpected HTTP request")
}

func (t *closeIdleTrackingRoundTripper) CloseIdleConnections() {
	t.closeCalls.Add(1)
}

func newTrackedRelayDestination() (*relayDestination, *closeIdleTrackingRoundTripper) {
	transport := new(closeIdleTrackingRoundTripper)
	client := new(http.Client)
	client.Transport = transport
	return &relayDestination{
		organizationID:       "",
		projectID:            uuid.Nil,
		endpoint:             "",
		headers:              nil,
		httpClient:           client,
		signalName:           "",
		includeSensitiveData: false,
	}, transport
}

func newRelayRouteTest(t *testing.T, endpointPath string) (*pgxpool.Pool, *encryption.Client, *signalRelay) {
	t.Helper()
	db := newTestDatabase(t)
	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return db, enc, newSignalRelay(db, enc, policy, endpointPath, "test")
}

func createRelayTestProject(t *testing.T, db *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()
	slug := "relay-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Relay Test",
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

func encryptRelayTestHeaders(t *testing.T, enc *encryption.Client, headers map[string]string) pgtype.Text {
	t.Helper()
	if headers == nil {
		headers = map[string]string{}
	}
	encoded, err := json.Marshal(headers)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt(encoded)
	require.NoError(t, err)
	return pgtype.Text{String: ciphertext, Valid: true}
}

func createRelayTestDestination(
	t *testing.T,
	db *pgxpool.Pool,
	organizationID string,
	projectID uuid.UUID,
	endpointURL string,
	headersEncrypted pgtype.Text,
	sensitiveData string,
) dataexportsrepo.OtelDestination {
	t.Helper()
	destination, err := dataexportsrepo.New(db).CreateOtelDestination(t.Context(), dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		Name:             "Relay collector",
		EndpointUrl:      endpointURL,
		HeadersEncrypted: headersEncrypted,
		SensitiveData:    pgtype.Text{String: sensitiveData, Valid: true},
	})
	require.NoError(t, err)
	return destination
}

func createRelayTestRoute(
	t *testing.T,
	db *pgxpool.Pool,
	organizationID string,
	projectID uuid.UUID,
	dataSource string,
	enabled bool,
	destinationID uuid.NullUUID,
) dataexportsrepo.DataExportRoute {
	t.Helper()
	route, err := dataexportsrepo.New(db).CreateDataExportRoute(t.Context(), dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    organizationID,
		ProjectID:         projectID,
		DataSource:        dataSource,
		Enabled:           enabled,
		OtelDestinationID: destinationID,
	})
	require.NoError(t, err)
	return route
}

func relayTestRouteKey(organizationID, projectID string) relayRouteKey {
	return relayRouteKey{
		organizationID: organizationID,
		projectID:      uuid.MustParse(projectID),
	}
}

func TestClassifyRelayStatusDistinguishesRetryableFailures(t *testing.T) {
	t.Parallel()

	reason, retryable := classifyRelayStatus(http.StatusServiceUnavailable)
	require.Equal(t, relayReasonHTTP5xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusRequestTimeout)
	require.Equal(t, relayReasonHTTP4xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusTooManyRequests)
	require.Equal(t, relayReasonHTTP4xx, reason)
	require.True(t, retryable)

	reason, retryable = classifyRelayStatus(http.StatusUnauthorized)
	require.Equal(t, relayReasonPermanentHTTPError, reason)
	require.False(t, retryable)
}
