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
	// sessionStatus tracks each session's state so the one-IN_PROGRESS-at-a-
	// time rule and the cancel path can be exercised.
	sessionStatus map[string]string
	// ignoreStatusFilter makes the list endpoint return every session
	// regardless of ?status=, standing in for an API that silently drops an
	// unknown query param.
	ignoreStatusFilter bool
	// failComplete rejects the completing action, stranding the session
	// mid-push the way a crashed or timed-out run does.
	failComplete bool
	// rejectRecordID makes the upload endpoint reject that record the way
	// the real API rejects a schema-validation failure: the response stays
	// 2xx and the rejection rides in the per-record result's "error" field.
	rejectRecordID string
	// records is the authoritative dataset: replaced wholesale when a
	// session completes, exactly like the real API.
	records []map[string]any
	// uploadRequests counts record-upload posts, for batching assertions.
	uploadRequests int
	completed      []string
	canceled       []string
	// deletedRecords logs ids removed via the record-delete endpoint, the
	// empty-fleet clear path.
	deletedRecords []string

	server *httptest.Server
}

func newFakeDrata(t *testing.T) *fakeDrata {
	t.Helper()
	f := &fakeDrata{
		t:                  t,
		apiKey:             "test-api-key",
		resourceIDs:        []string{testResourceID},
		mu:                 sync.Mutex{},
		sessions:           map[string][]map[string]any{},
		sessionStatus:      map[string]string{},
		ignoreStatusFilter: false,
		failComplete:       false,
		rejectRecordID:     "",
		records:            nil,
		uploadRequests:     0,
		completed:          nil,
		canceled:           nil,
		deletedRecords:     nil,
		server:             nil,
	}

	connBase := "/public/v2/custom-connections/" + testConnectionID
	sessionsList := connBase + "/resources/" + testResourceID + "/sessions"
	sessionsBase := sessionsList + "/"

	mux := http.NewServeMux()
	// Registered without the trailing slash so the list read does not fall
	// into ServeMux's subtree redirect onto the upload handler.
	mux.HandleFunc(sessionsList, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(f.t, http.MethodGet, r.Method)
		if !f.authorized(w, r) {
			return
		}
		wanted := r.URL.Query().Get("status")
		f.mu.Lock()
		// Items mirror the shape observed from the production API: a numeric
		// "id" alongside the caller-chosen "sessionId", extra timestamp
		// fields, and a pagination envelope. The provider once decoded "id"
		// as a string and the mismatch broke every push.
		listed := make([]string, 0, len(f.sessionStatus))
		for id, status := range f.sessionStatus {
			if f.ignoreStatusFilter || wanted == "" || status == wanted {
				listed = append(listed, fmt.Sprintf(`{"id": %d, "sessionId": %q, "status": %q, "activatedAt": null}`, len(listed)+1, id, status))
			}
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data": [%s], "pagination": {"cursor": null}}`, strings.Join(listed, ","))
	})
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
			assert.Contains(f.t, []string{"complete", "cancel"}, action.Action)
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
			if action.Action == "cancel" {
				// Cancelling discards the staged records and leaves the live
				// dataset untouched.
				delete(f.sessions, sessionID)
				f.sessionStatus[sessionID] = "CANCELLED"
				f.canceled = append(f.canceled, sessionID)
				w.WriteHeader(http.StatusOK)
				return
			}
			if f.failComplete {
				// The session stays IN_PROGRESS, which is precisely what
				// strands it for every later push.
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if len(uploaded) == 0 {
				// Mirrors production: completing a session with no records is
				// refused with 422 "Cannot complete a session with no data
				// records" — which is why the empty-fleet path must delete
				// records instead of pushing an empty session.
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			// Completion makes the session the authoritative dataset.
			f.records = uploaded
			f.sessionStatus[sessionID] = "ACTIVE"
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
		// "Only one session can be IN_PROGRESS at a time per
		// connection/resource." Enforcing it here is what makes a stranded
		// session bite: without recovery, one partial push wedges every
		// later one. Drata documents no status for the collision, so the
		// provider must not depend on this particular code.
		for id, status := range f.sessionStatus {
			if id != rest && status == "IN_PROGRESS" {
				f.mu.Unlock()
				w.WriteHeader(http.StatusConflict)
				return
			}
		}
		// Per-record results mirror the real response: overall 2xx with a
		// bare array whose rejected entries carry an "error" object (their
		// statusCode still reads 201) and the record only under "data".
		// Rejected records are not staged into the session.
		results := make([]string, 0, len(payload.Data))
		accepted := make([]map[string]any, 0, len(payload.Data))
		for _, rec := range payload.Data {
			id, _ := rec["id"].(string)
			if f.rejectRecordID != "" && id == f.rejectRecordID {
				results = append(results, fmt.Sprintf(`{"statusCode": 201, "error": {"message": "Schema validation failed for data", "code": 28022}, "data": {"id": %q}}`, id))
				continue
			}
			results = append(results, fmt.Sprintf(`{"id": %q, "statusCode": 201, "data": {"id": %q}}`, id, id))
			accepted = append(accepted, rec)
		}
		// Assignment (not just append) so an empty first upload still
		// creates the session, mirroring implicit session creation.
		f.sessions[rest] = append(f.sessions[rest], accepted...)
		if _, seen := f.sessionStatus[rest]; !seen {
			f.sessionStatus[rest] = "IN_PROGRESS"
		}
		f.uploadRequests++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, "[%s]", strings.Join(results, ","))
	})

	recordsBase := connBase + "/resources/" + testResourceID + "/records"
	mux.HandleFunc(recordsBase, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(f.t, http.MethodGet, r.Method)
		if !f.authorized(w, r) {
			return
		}
		f.mu.Lock()
		items := make([]string, 0, len(f.records))
		for _, rec := range f.records {
			items = append(items, fmt.Sprintf(`{"id": %q, "data": {"id": %q}}`, rec["id"], rec["id"]))
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data": [%s], "pagination": {"cursor": null}}`, strings.Join(items, ","))
	})
	mux.HandleFunc(recordsBase+"/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(f.t, http.MethodDelete, r.Method)
		if !f.authorized(w, r) {
			return
		}
		id := strings.TrimPrefix(r.URL.Path, recordsBase+"/")
		f.mu.Lock()
		defer f.mu.Unlock()
		kept := make([]map[string]any, 0, len(f.records))
		found := false
		for _, rec := range f.records {
			if !found && rec["id"] == id {
				found = true
				continue
			}
			kept = append(kept, rec)
		}
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.records = kept
		f.deletedRecords = append(f.deletedRecords, id)
		w.WriteHeader(http.StatusNoContent)
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
			ExternalID:      fmt.Sprintf("dev-%04d", i+1),
			SerialNumber:    fmt.Sprintf("SER%04d", i+1),
			Hostname:        fmt.Sprintf("mac-%04d", i+1),
			UserEmail:       fmt.Sprintf("user%d@example.test", i+1),
			AgentActive:     i%2 == 0,
			AgentLastSeenAt: time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC),
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
	require.Equal(t, true, first["agentActive"])
	require.Equal(t, "2026-07-28T09:00:00Z", first["agentLastSeenAt"])
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
	require.Len(t, fake.deletedRecords, 4, "the empty fleet clears via record deletion — the API refuses to complete an empty session")
	require.Len(t, fake.completed, 1, "only the initial non-empty push completes a session")
}

// TestPushSurfacesPerRecordRejection pins the trap where an upload's 2xx
// response hides schema-validation rejections in per-record results: a push
// that ignored them would complete a session missing part of the fleet and
// publish it as the authoritative dataset.
func TestPushSurfacesPerRecordRejection(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	fake.mu.Lock()
	fake.rejectRecordID = "dev-0002"
	fake.mu.Unlock()

	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3))
	require.ErrorContains(t, err, "dev-0002")
	require.ErrorContains(t, err, "Schema validation failed")
	require.Empty(t, fake.completed, "a partially rejected upload must not publish the session")
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
			ExternalID:      "dev-unassigned",
			SerialNumber:    "SERX",
			Hostname:        "mac-x",
			UserEmail:       "",
			AgentActive:     false,
			AgentLastSeenAt: time.Time{},
		}},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.records, 1)
	record := fake.records[0]
	require.Empty(t, record["assignedUserEmail"])
	require.Equal(t, false, record["agentActive"])
	_, present := record["agentLastSeenAt"]
	require.False(t, present, "a never-seen agent omits the field entirely — the schema types it as a plain string, so null or a zero timestamp would overclaim")
}

func TestPushCancelsStrandedSession(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	// A crashed earlier run: records staged into a session that was never
	// completed. Drata allows only one IN_PROGRESS session per resource, so
	// without recovery every later push collides with this one forever.
	fake.mu.Lock()
	fake.sessions["gram-stranded"] = []map[string]any{{"id": "dev-old"}}
	fake.sessionStatus["gram-stranded"] = "IN_PROGRESS"
	fake.mu.Unlock()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(2)))

	require.Equal(t, []string{"gram-stranded"}, fake.canceled, "the stranded session is cancelled, not completed: completing it would publish a partial fleet as authoritative")
	require.Len(t, fake.completed, 1)
	require.NotContains(t, fake.completed, "gram-stranded")
	require.Len(t, fake.records, 2, "the live dataset is the new snapshot alone")
}

// TestStrandedSweepDecodesSessionListShapes pins the sweep's decode against
// the response shapes the endpoint is undocumented enough to serve. The
// production API returns numeric session "id"s inside a data/pagination
// envelope — decoding "id" as a string once broke every push — and an empty
// sweep may surface as data: null or a missing data key rather than [].
func TestStrandedSweepDecodesSessionListShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		// wantCancel is the session identifier the sweep must cancel, or ""
		// when the listing holds nothing cancellable.
		wantCancel string
	}{
		{name: "envelope with null data", body: `{"data": null, "pagination": {"cursor": null}}`, wantCancel: ""},
		{name: "envelope without data key", body: `{"pagination": {"cursor": null}}`, wantCancel: ""},
		{name: "bare empty array", body: `[]`, wantCancel: ""},
		{name: "bare array with a strand", body: `[{"sessionId": "gram-old", "status": "IN_PROGRESS"}]`, wantCancel: "gram-old"},
		{
			name:       "production envelope shape",
			body:       `{"data": [{"id": 2, "sessionId": "gram-old", "status": "IN_PROGRESS", "createdAt": "2026-07-30T16:55:20.756Z", "activatedAt": null}], "pagination": {"cursor": null}}`,
			wantCancel: "gram-old",
		},
		{
			// No sessionId at all: the identifier must fall back to the
			// numeric id, stringified, because it names the cancel URL.
			name:       "numeric id only",
			body:       `{"data": [{"id": 7, "status": "IN_PROGRESS"}]}`,
			wantCancel: "7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var canceled []string
			mux := http.NewServeMux()
			mux.HandleFunc("/resource/sessions", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "IN_PROGRESS", r.URL.Query().Get("status"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			})
			mux.HandleFunc("/resource/sessions/", func(w http.ResponseWriter, r *http.Request) {
				id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/resource/sessions/"), "/actions")
				assert.True(t, ok, "only action posts are expected here")
				canceled = append(canceled, id)
				w.WriteHeader(http.StatusCreated)
			})
			server := httptest.NewTLSServer(mux)
			t.Cleanup(server.Close)

			s := &sink{client: server.Client(), regions: map[string]string{"us": server.URL}}
			require.NoError(t, s.cancelStrandedSessions(t.Context(), providers.Credentials{fieldAPIKey: "k"}, server.URL+"/resource"))

			if tc.wantCancel == "" {
				require.Empty(t, canceled)
			} else {
				require.Equal(t, []string{tc.wantCancel}, canceled)
			}
		})
	}
}

// TestFlexIDAcceptsOnlyScalarIDs pins the decode boundary: ids become URL
// path segments (resource and session-cancel requests), so a non-scalar id
// must fail the decode loudly rather than serialize into a bogus API call.
func TestFlexIDAcceptsOnlyScalarIDs(t *testing.T) {
	t.Parallel()

	var ref sessionRef
	require.Error(t, json.Unmarshal([]byte(`{"id": {"nested": 1}}`), &ref))
	require.Error(t, json.Unmarshal([]byte(`{"id": true}`), &ref))
	require.Error(t, json.Unmarshal([]byte(`{"id": ["7"]}`), &ref))

	var quoted sessionRef
	require.NoError(t, json.Unmarshal([]byte(`{"id": "a\"b"}`), &quoted))
	require.Equal(t, `a"b`, string(quoted.ID), "string escapes decode as JSON, not by quote-trimming")

	var numeric sessionRef
	require.NoError(t, json.Unmarshal([]byte(`{"id": 42}`), &numeric))
	require.Equal(t, "42", string(numeric.ID))

	var null sessionRef
	require.NoError(t, json.Unmarshal([]byte(`{"id": null}`), &null))
	require.Empty(t, string(null.ID))
}

func TestPushRecoversAfterPartialFailure(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	// Fail the completing action so the first attempt strands its session
	// mid-push, exactly as a crashed or timed-out run would.
	fake.mu.Lock()
	fake.failComplete = true
	fake.mu.Unlock()
	require.Error(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3)))

	fake.mu.Lock()
	fake.failComplete = false
	fake.mu.Unlock()

	// The retry must clear the strand and publish cleanly rather than wedge.
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3)))
	require.Len(t, fake.canceled, 1, "the first attempt's session is reclaimed")
	require.Len(t, fake.records, 3)
}

func TestStrandedSweepNeverCancelsActiveSession(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	// Publish a real dataset, then make the list endpoint ignore ?status=.
	// An API that drops the filter would hand back the ACTIVE session, and
	// cancelling that would destroy the live evidence.
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(2)))
	fake.mu.Lock()
	fake.ignoreStatusFilter = true
	fake.mu.Unlock()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(4)))
	require.Empty(t, fake.canceled, "only IN_PROGRESS sessions may be cancelled, whatever the listing returns")
	require.Len(t, fake.records, 4)
}

// TestPushCoverageEmitsAttestationPerRecord pins the field this integration's
// whole compliance claim rests on, at the JSON boundary the CUSTOMER's Drata
// record schema must match. Both strengths must survive one push: a machine
// whose agent cannot read a serial stays user-attested even for an org on
// device-level matching.
func TestPushCoverageEmitsAttestationPerRecord(t *testing.T) {
	t.Parallel()

	fake := newFakeDrata(t)
	s := fake.newSink(t)

	seen := time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC)
	snapshot := providers.CoverageSnapshot{
		OrganizationID: "org-test",
		GeneratedAt:    time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC),
		Devices: []providers.CoverageDevice{
			{
				ExternalID: "dev-attested", SerialNumber: "SER-A", Hostname: "mac-a",
				UserEmail: "a@example.test", AgentActive: true,
				AgentAttestation: providers.AttestationDevice, AgentLastSeenAt: seen,
			},
			{
				ExternalID: "dev-user-only", SerialNumber: "SER-B", Hostname: "mac-b",
				UserEmail: "b@example.test", AgentActive: true,
				AgentAttestation: providers.AttestationUser, AgentLastSeenAt: seen,
			},
		},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.records, 2)
	byID := map[string]map[string]any{}
	for _, r := range fake.records {
		id, ok := r["id"].(string)
		require.True(t, ok)
		byID[id] = r
	}

	attested := byID["dev-attested"]
	require.Equal(t, "device", attested["agentAttestation"],
		"a serial-matched device must publish the strong claim under the exact key the docs tell auditors to test")
	require.Equal(t, true, attested["agentActive"])
	require.Equal(t, "2026-07-29T09:00:00Z", attested["agentLastSeenAt"])

	userOnly := byID["dev-user-only"]
	require.Equal(t, "user", userOnly["agentAttestation"],
		"an email-matched device must publish the weaker claim in the same push")
	require.Equal(t, true, userOnly["agentActive"])

	// The removed field names must not linger anywhere in the payload.
	for _, r := range fake.records {
		require.NotContains(t, r, "assignedUserAgentActive")
		require.NotContains(t, r, "assignedUserAgentLastSeenAt")
	}
}
