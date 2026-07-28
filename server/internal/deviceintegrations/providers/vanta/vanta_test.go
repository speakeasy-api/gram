package vanta

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

const testResourceID = "res-abc123"

// fakeVanta is an httptest stand-in for the private-integration API: a
// client-credentials token endpoint enforcing Vanta's one-active-token rule,
// and a full-state resource PUT with accepted/rejected counts.
type fakeVanta struct {
	t *testing.T

	mu           sync.Mutex
	clientID     string
	clientSecret string
	// activeToken is the single valid token; minting a new one revokes it,
	// exactly like the real API.
	activeToken  string
	tokenMints   int
	putRequests  int
	rejectNext   int
	resources    []map[string]any
	lastEnvelope map[string]any

	server *httptest.Server
}

func newFakeVanta(t *testing.T) *fakeVanta {
	t.Helper()
	f := &fakeVanta{
		t:            t,
		mu:           sync.Mutex{},
		clientID:     "test-client-id",
		clientSecret: "test-client-secret",
		activeToken:  "",
		tokenMints:   0,
		putRequests:  0,
		rejectNext:   0,
		resources:    nil,
		lastEnvelope: nil,
		server:       nil,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"), "every request carries the integration User-Agent")
		var body struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			GrantType    string `json:"grant_type"`
			Scope        string `json:"scope"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "client_credentials", body.GrantType)
		assert.Contains(t, body.Scope, "connectors.self:write-resource", "least-privilege scope only")

		f.mu.Lock()
		defer f.mu.Unlock()
		if body.ClientID != f.clientID || body.ClientSecret != f.clientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.tokenMints++
		// One active token per application: the new mint revokes the old.
		f.activeToken = fmt.Sprintf("vat_token_%d", f.tokenMints)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": f.activeToken,
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/v1/resources/custom_resource", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method, "sync is a full-state PUT")
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"))

		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+f.activeToken || f.activeToken == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var envelope struct {
			ResourceID string           `json:"resourceId"`
			Resources  []map[string]any `json:"resources"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&envelope))
		f.putRequests++
		f.lastEnvelope = map[string]any{"resourceId": envelope.ResourceID}

		rejected := f.rejectNext
		f.rejectNext = 0
		accepted := len(envelope.Resources) - rejected
		if rejected == 0 {
			// Full-state replace: this PUT is now the authoritative set.
			f.resources = envelope.Resources
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{"accepted": accepted, "rejected": rejected},
		})
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// revokeActiveToken simulates an external mint (another worker or a
// connection test elsewhere) revoking the sink's cached token.
func (f *fakeVanta) revokeActiveToken() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeToken = "vat_externally_rotated"
}

func (f *fakeVanta) creds() providers.Credentials {
	return providers.Credentials{fieldClientID: f.clientID, fieldClientSecret: f.clientSecret}
}

func (f *fakeVanta) settings() providers.Settings {
	return providers.Settings{fieldResourceID: testResourceID}
}

// newSink returns a sink wired to the fake's TLS client (unit tests use the
// httptest client directly; production wiring goes through guardian).
func (f *fakeVanta) newSink() *sink {
	return &sink{client: f.server.Client(), baseURL: f.server.URL, mu: sync.Mutex{}, token: "", tokenExpiry: time.Time{}}
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

func TestPushCoverageFullStateReplace(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3)))
	require.Equal(t, 1, fake.putRequests, "the full fleet travels in exactly one PUT")
	require.Len(t, fake.resources, 3)
	require.Equal(t, testResourceID, fake.lastEnvelope["resourceId"])

	first := fake.resources[0]
	require.Equal(t, "dev-0001", first["uniqueId"])
	require.Equal(t, "mac-0001", first["displayName"])
	props, ok := first["customProperties"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "SER0001", props["serial_number"])
	require.Equal(t, "user1@example.test", props["assigned_user_email"])
	require.Equal(t, true, props["assigned_user_agent_active"])
	require.Equal(t, "2026-07-28T09:00:00Z", props["assigned_user_agent_last_seen_at"])
	// The attestation is per assigned user: no device-level claim may leak
	// into the property names.
	for key := range props {
		require.NotContains(t, strings.ToLower(key), "monitored")
	}

	// A later push wholesale-replaces the set.
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(2)))
	require.Len(t, fake.resources, 2)
	require.Equal(t, 1, fake.tokenMints, "the cached token serves the whole run; minting per request would revoke it")
}

func TestPushCoverageEmptyFleetClearsEvidence(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(4)))
	require.Len(t, fake.resources, 4)

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(0)))
	require.Empty(t, fake.resources, "an empty full-state PUT truthfully clears stale evidence")
}

func TestTestConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	require.NoError(t, fake.newSink().TestConnection(t.Context(), fake.creds(), fake.settings()))

	bad := providers.Credentials{fieldClientID: "wrong", fieldClientSecret: "wrong-secret"}
	err := fake.newSink().TestConnection(t.Context(), bad, fake.settings())
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err), "credential rejections classify as auth errors")

	err = fake.newSink().TestConnection(t.Context(), fake.creds(), providers.Settings{fieldResourceID: "  "})
	require.ErrorContains(t, err, "not configured")
}

func TestRejectedRecordsFailLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	fake.mu.Lock()
	fake.rejectNext = 2
	fake.mu.Unlock()

	// Full-state semantics make rejections dangerous: a rejected record is
	// absent from the authoritative set, which reads as "device gone".
	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(5))
	require.ErrorContains(t, err, "rejected 2 of 5")
}

func TestExternalTokenRevocationClassifiesAsAuthAndRecovers(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))

	// Another mint elsewhere revokes our cached token (Vanta allows one
	// active token per application).
	fake.revokeActiveToken()
	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1))
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err))

	// The cache was dropped, so the next push re-mints and succeeds — the
	// sync scheduler's retry recovers without operator action.
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))
}

func TestUnassignedDeviceResource(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	snapshot := providers.CoverageSnapshot{
		OrganizationID: "org-test",
		GeneratedAt:    time.Date(2026, 7, 28, 10, 30, 0, 0, time.UTC),
		Devices: []providers.CoverageDevice{{
			ExternalID:                  "dev-unassigned",
			SerialNumber:                "SERX",
			Hostname:                    "",
			UserEmail:                   "",
			AssignedUserAgentActive:     false,
			AssignedUserAgentLastSeenAt: time.Time{},
		}},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.resources, 1)
	resource := fake.resources[0]
	require.Equal(t, "dev-unassigned", resource["displayName"], "a hostname-less device falls back to its external id")
	props, ok := resource["customProperties"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, props["assigned_user_email"])
	require.Equal(t, false, props["assigned_user_agent_active"])
	_, present := props["assigned_user_agent_last_seen_at"]
	require.False(t, present, "a never-seen agent omits the property — null or a zero timestamp would overclaim")
}
