package drata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
)

const (
	testConnectionID = "8711"
	testResourceID   = "42"
)

// fakeDrata is an httptest stand-in for the Custom Connections API: the
// connection read with resource expansion, session-batched record uploads,
// and the completing action with authoritative-replace semantics.
type fakeDrata struct {
	t *testing.T

	apiKey string
	// resourceIDs is the connection's customResources id list, JSON-encoded
	// verbatim so tests can exercise numeric ids, string ids, none, or
	// several.
	resourceIDs []string

	mu sync.Mutex
	// sessions accumulates uploaded records per session id.
	sessions map[string][]map[string]any
	// records is the authoritative dataset: replaced wholesale when a
	// session completes, exactly like the real API.
	records []map[string]any
	// uploadRequests counts record-upload posts, for batching assertions.
	uploadRequests int
	completed      []string

	server *httptest.Server
}

func newFakeDrata(t *testing.T) *fakeDrata {
	t.Helper()
	f := &fakeDrata{
		t:              t,
		apiKey:         "test-api-key",
		resourceIDs:    []string{testResourceID},
		mu:             sync.Mutex{},
		sessions:       map[string][]map[string]any{},
		records:        nil,
		uploadRequests: 0,
		completed:      nil,
		server:         nil,
	}

	connBase := "/public/v2/custom-connections/" + testConnectionID
	sessionsBase := connBase + "/resources/" + testResourceID + "/sessions/"

	mux := http.NewServeMux()
	mux.HandleFunc(connBase, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(f.t, http.MethodGet, r.Method)
		assert.Equal(f.t, userAgent, r.Header.Get("User-Agent"), "every request carries the integration User-Agent")
		assert.Equal(f.t, "customResources", r.URL.Query().Get("expand[]"), "resource discovery must expand customResources")
		if !f.authorized(w, r) {
			return
		}
		f.mu.Lock()
		resources := make([]string, 0, len(f.resourceIDs))
		for _, id := range f.resourceIDs {
			resources = append(resources, fmt.Sprintf(`{"id": %s}`, id))
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id": %s, "name": "Gram Device Agent Coverage", "customResources": [%s]}`, testConnectionID, strings.Join(resources, ","))
	})
	mux.HandleFunc(sessionsBase, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(f.t, http.MethodPost, r.Method)
		if !f.authorized(w, r) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, sessionsBase)

		if sessionID, ok := strings.CutSuffix(rest, "/actions"); ok {
			var action struct {
				Action string `json:"action"`
			}
			assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&action))
			assert.Equal(f.t, "complete", action.Action)
			f.mu.Lock()
			defer f.mu.Unlock()
			// Sessions exist only once an upload created them; completing an
			// unknown session must fail, or the provider's empty-fleet path
			// (which relies on an empty upload creating the session) would
			// pass vacuously here while failing against the real API.
			uploaded, ok := f.sessions[sessionID]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			// Completion makes the session the authoritative dataset.
			f.records = uploaded
			f.completed = append(f.completed, sessionID)
			w.WriteHeader(http.StatusOK)
			return
		}

		assert.NotContains(f.t, rest, "/", "upload path carries only the session id")
		assert.GreaterOrEqual(f.t, len(rest), 3, "session id must be at least 3 chars")
		assert.LessOrEqual(f.t, len(rest), 64, "session id must be at most 64 chars")
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&payload))
		assert.LessOrEqual(f.t, len(payload.Data), recordBatchSize, "uploads must respect the batch cap")
		f.mu.Lock()
		// Assignment (not just append) so an empty first upload still
		// creates the session, mirroring implicit session creation.
		f.sessions[rest] = append(f.sessions[rest], payload.Data...)
		f.uploadRequests++
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})

	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDrata) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+f.apiKey {
		w.WriteHeader(http.StatusUnauthorized)
		return false
	}
	return true
}

func (f *fakeDrata) creds() providers.Credentials {
	return providers.Credentials{fieldAPIKey: f.apiKey}
}

func (f *fakeDrata) settings() providers.Settings {
	return providers.Settings{fieldRegion: "us", fieldConnectionID: testConnectionID}
}

// newSink returns a sink wired to the fake's TLS client, with the region
// allowlist mapping "us" to the fake (unit tests use the httptest client
// directly; production wiring goes through guardian).
func (f *fakeDrata) newSink(t *testing.T) *sink {
	t.Helper()
	return &sink{client: f.server.Client(), regions: map[string]string{"us": f.server.URL}}
}

func snapshotOf(deviceCount int) providers.CoverageSnapshot {
	devices := make([]providers.CoverageDevice, 0, deviceCount)
	for i := range deviceCount {
		devices = append(devices, providers.CoverageDevice{
			ExternalID:                  fmt.Sprintf("dev-%04d", i+1),
			SerialNumber:                fmt.Sprintf("SER%04d", i+1),
			Hostname:                    fmt.Sprintf("mac-%04d", i+1),
			UserEmail:                   fmt.Sprintf("user%d@example.test", i+1),
			AssignedUserAgentActive:     i%2 == 0,
			AssignedUserAgentLastSeenAt: time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC),
		})
	}
	return providers.CoverageSnapshot{
		OrganizationID: "org-test",
		GeneratedAt:    time.Date(2026, 7, 28, 10, 30, 0, 0, time.UTC),
		Devices:        devices,
	}
}

func TestPushCoverageReplacesSnapshot(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3)))

	require.Len(t, fake.records, 3)
	first := fake.records[0]
	require.Equal(t, "dev-0001", first["id"])
	require.Equal(t, "SER0001", first["serialNumber"])
	require.Equal(t, "mac-0001", first["hostname"])
	require.Equal(t, "user1@example.test", first["assignedUserEmail"])
	require.Equal(t, true, first["assignedUserAgentActive"])
	require.Equal(t, "2026-07-28T09:00:00Z", first["assignedUserAgentLastSeenAt"])
	// The attestation is per assigned user: no device-level claim may leak
	// into the schema.
	for key := range first {
		require.NotContains(t, strings.ToLower(key), "monitored")
	}
	require.Len(t, fake.completed, 1)
	require.Regexp(t, "^gram-[a-z0-9]+$", fake.completed[0], "session ids stay within Drata's allowed charset")
}

func TestPushCoverageBatchesLargeFleets(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1250)))

	require.Len(t, fake.records, 1250)
	require.Equal(t, 3, fake.uploadRequests, "1250 records at batch size 500 is exactly three uploads")
}

func TestPushCoverageRetryDoesNotDuplicate(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	snapshot := snapshotOf(5)
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.records, 5, "a retried push of the same snapshot must not duplicate records")
}

func TestPushCoverageEmptyFleetClearsEvidence(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(4)))
	require.Len(t, fake.records, 4)

	empty := snapshotOf(0)
	empty.GeneratedAt = empty.GeneratedAt.Add(time.Hour)
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), empty))
	require.Empty(t, fake.records, "an empty fleet replaces stale evidence with the truthful empty set")
}

func TestTestConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)
	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))

	bad := providers.Credentials{fieldAPIKey: "wrong-key"}
	err := s.TestConnection(t.Context(), bad, fake.settings())
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err), "credential rejections classify as auth errors")

	missing := providers.Settings{fieldRegion: "us", fieldConnectionID: "999999"}
	err = s.TestConnection(t.Context(), fake.creds(), missing)
	require.Error(t, err)
	require.False(t, providers.IsAuthError(err), "an unknown connection id is a config error, not an auth error")
}

func TestRegionValidation(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	err := s.TestConnection(t.Context(), fake.creds(), providers.Settings{fieldRegion: "mars", fieldConnectionID: testConnectionID})
	require.ErrorContains(t, err, "region must be one of")

	err = s.TestConnection(t.Context(), fake.creds(), providers.Settings{fieldRegion: "", fieldConnectionID: testConnectionID})
	require.ErrorContains(t, err, "not configured")

	// Region matching is case- and whitespace-insensitive.
	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), providers.Settings{fieldRegion: " US ", fieldConnectionID: testConnectionID}))
}

func TestConnectionResourceCardinality(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	fake.resourceIDs = nil
	err := s.TestConnection(t.Context(), fake.creds(), fake.settings())
	require.ErrorContains(t, err, "no resource")

	// Guessing among several resources risks wholesale-replacing the wrong
	// one's records, so the sink must refuse instead.
	fake.resourceIDs = []string{"42", "43"}
	err = s.TestConnection(t.Context(), fake.creds(), fake.settings())
	require.ErrorContains(t, err, "dedicated to Gram")
}

func TestStringResourceIDTolerated(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	// The reference documents numeric resource ids, but a string-typed id
	// must not brick the integration.
	fake.resourceIDs = []string{`"` + testResourceID + `"`}
	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))
	require.Len(t, fake.records, 1)
}

func TestUnassignedDeviceRecord(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	snapshot := providers.CoverageSnapshot{
		OrganizationID: "org-test",
		GeneratedAt:    time.Date(2026, 7, 28, 10, 30, 0, 0, time.UTC),
		Devices: []providers.CoverageDevice{{
			ExternalID:                  "dev-unassigned",
			SerialNumber:                "SERX",
			Hostname:                    "mac-x",
			UserEmail:                   "",
			AssignedUserAgentActive:     false,
			AssignedUserAgentLastSeenAt: time.Time{},
		}},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.records, 1)
	record := fake.records[0]
	require.Empty(t, record["assignedUserEmail"])
	require.Equal(t, false, record["assignedUserAgentActive"])
	_, present := record["assignedUserAgentLastSeenAt"]
	require.False(t, present, "a never-seen agent omits the field entirely — the schema types it as a plain string, so null or a zero timestamp would overclaim")
}
