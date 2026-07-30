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
	mu           sync.Mutex
	clientID     string
	clientSecret string
	// activeToken is the single valid token; minting a new one revokes it,
	// exactly like the real API.
	activeToken string
	tokenMints  int
	// expiresIn is the token lifetime the fake reports.
	expiresIn   int
	putRequests int
	// rejectNext makes the next PUT fail the whole request with a 4xx, as
	// Vanta does for any schema violation (its sync is all-or-nothing).
	rejectNext bool
	// respondEmpty makes the next PUT return a bare {} — a drifted response
	// envelope that must not read as success.
	respondEmpty   bool
	resources      []map[string]any
	lastResourceID string

	server *httptest.Server
}

func newFakeVanta(t *testing.T) *fakeVanta {
	t.Helper()
	f := &fakeVanta{
		mu:             sync.Mutex{},
		clientID:       "test-client-id",
		clientSecret:   "test-client-secret",
		activeToken:    "",
		tokenMints:     0,
		expiresIn:      3600,
		putRequests:    0,
		rejectNext:     false,
		respondEmpty:   false,
		resources:      nil,
		lastResourceID: "",
		server:         nil,
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
			"expires_in":   f.expiresIn,
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
		f.lastResourceID = envelope.ResourceID

		// Mirror Vanta's base-schema validation: the CustomResource base
		// requires uniqueId, displayName, and externalUrl at the top level,
		// and — because the console cannot author an optionalProperties
		// schema — every declared custom property, including
		// agent_last_seen_at, is required. A real omission 400s at sync; the
		// fake asserts so a sink regression fails here rather than in prod.
		for _, res := range envelope.Resources {
			for _, base := range []string{"uniqueId", "displayName", "externalUrl"} {
				_, ok := res[base]
				assert.Truef(t, ok, "resource missing required base field %q", base)
			}
			props, _ := res["customProperties"].(map[string]any)
			_, ok := props["agent_last_seen_at"]
			assert.True(t, ok, "customProperties missing required agent_last_seen_at")
		}

		if f.respondEmpty {
			f.respondEmpty = false
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
			return
		}

		if f.rejectNext {
			// Vanta's full-state PUT is all-or-nothing: any schema violation
			// fails the whole request with a 4xx and an {"error": ...} body,
			// leaving the authoritative set untouched.
			f.rejectNext = false
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"/0/customProperties: must have property 'hostname'"}`))
			return
		}

		// Full-state replace: this PUT is now the authoritative set.
		f.resources = envelope.Resources
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
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
	return newSink(f.server.Client(), f.server.URL)
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

func TestPushCoverageFullStateReplace(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3)))
	require.Equal(t, 1, fake.putRequests, "the full fleet travels in exactly one PUT")
	require.Len(t, fake.resources, 3)
	require.Equal(t, testResourceID, fake.lastResourceID)

	first := fake.resources[0]
	require.Equal(t, "dev-0001", first["uniqueId"])
	require.Equal(t, "mac-0001", first["displayName"])
	require.Equal(t, "https://app.getgram.ai", first["externalUrl"], "Vanta's base schema requires externalUrl on every resource")
	props, ok := first["customProperties"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "SER0001", props["serial_number"])
	require.Equal(t, "user1@example.test", props["assigned_user_email"])
	require.Equal(t, true, props["agent_active"])
	require.Equal(t, "2026-07-28T09:00:00Z", props["agent_last_seen_at"])
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
	fake.rejectNext = true
	fake.mu.Unlock()

	// Vanta's sync is all-or-nothing: a schema violation fails the whole PUT
	// with a 4xx, so no partial set is published as authoritative.
	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(5))
	require.ErrorContains(t, err, "failed with status 400")
	require.Empty(t, fake.resources, "a rejected push leaves the authoritative set untouched")
}

func TestExternalTokenRevocationRecoversInRun(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))

	// Another mint elsewhere revokes our cached token (Vanta allows one
	// active token per application). The push absorbs exactly one such
	// revocation by re-minting and retrying within the run — no spurious
	// auth rejection reaches the scheduler.
	fake.revokeActiveToken()
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(2)))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Equal(t, 2, fake.tokenMints, "one initial mint plus one recovery re-mint")
	require.Len(t, fake.resources, 2, "the retried PUT carried the full snapshot")
}

func TestShortTokenLifetimeStillCaches(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	fake.mu.Lock()
	fake.expiresIn = 20 // below 2x the 30s slack: the ttl/2 floor must hold
	fake.mu.Unlock()
	s := fake.newSink()

	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(1)))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Equal(t, 1, fake.tokenMints, "the cache serves back-to-back pushes even under a short lifetime — every extra mint revokes someone's token")
}

func TestDriftedSuccessEnvelopeFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	// A drifted 2xx envelope (bare {}, no "success" field) must not read as a
	// completed sync — a renamed or dropped field could otherwise let a
	// no-op response pass for a successful full-state replace.
	fake.mu.Lock()
	fake.respondEmpty = true
	fake.mu.Unlock()
	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(3))
	require.ErrorContains(t, err, "did not report success")

	// The empty-fleet edge: a clear (zero records) must also confirm success
	// from the body, not from the bare 2xx.
	fake.mu.Lock()
	fake.respondEmpty = true
	fake.mu.Unlock()
	err = s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshotOf(0))
	require.ErrorContains(t, err, "did not report success")
}

func TestSnapshotIntegrityGuards(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	empty := snapshotOf(1)
	empty.Devices[0].ExternalID = ""
	err := s.PushCoverage(t.Context(), fake.creds(), fake.settings(), empty)
	require.ErrorContains(t, err, "no external id")

	dup := snapshotOf(2)
	dup.Devices[1].ExternalID = dup.Devices[0].ExternalID
	err = s.PushCoverage(t.Context(), fake.creds(), fake.settings(), dup)
	require.ErrorContains(t, err, "duplicate device external id")
}

func TestDescriptorRegistered(t *testing.T) {
	t.Parallel()

	desc, ok := providers.Lookup(ProviderID)
	require.True(t, ok)
	require.True(t, desc.HasCapability(providers.CapabilityEvidenceSink))

	secretKeys := make([]string, 0, 2)
	for _, f := range desc.SecretFields() {
		secretKeys = append(secretKeys, f.Key)
	}
	require.ElementsMatch(t, []string{fieldClientID, fieldClientSecret}, secretKeys,
		"the OAuth pair is write-only; flipping a secret flag would echo credentials back through the readable settings document")
	require.Len(t, desc.Schedules, 1)
	require.Equal(t, providers.CapabilityEvidenceSink, desc.Schedules[0].Capability)
}

func TestUnassignedDeviceResource(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := fake.newSink()

	snapshot := providers.CoverageSnapshot{
		OrganizationID: "org-test",
		GeneratedAt:    time.Date(2026, 7, 28, 10, 30, 0, 0, time.UTC),
		Devices: []providers.CoverageDevice{{
			ExternalID:      "dev-unassigned",
			SerialNumber:    "SERX",
			Hostname:        "",
			UserEmail:       "",
			AgentActive:     false,
			AgentLastSeenAt: time.Time{},
		}},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	require.Len(t, fake.resources, 1)
	resource := fake.resources[0]
	require.Equal(t, "dev-unassigned", resource["displayName"], "a hostname-less device falls back to its external id")
	props, ok := resource["customProperties"].(map[string]any)
	require.True(t, ok)
	require.Empty(t, props["assigned_user_email"])
	require.Equal(t, false, props["agent_active"])
	require.Equal(t, "https://app.getgram.ai", resource["externalUrl"])
	lastSeen, present := props["agent_last_seen_at"]
	require.True(t, present, "the property is always present — Vanta's console cannot mark it optional, so an omitted field is rejected")
	require.Empty(t, lastSeen, "a never-seen agent sends an empty string — honestly unknown, not a fabricated timestamp")
}

// TestPushCoverageEmitsAttestationPerResource is the Vanta twin: the custom
// properties are a customer-declared schema, so the attestation key and both
// of its values must be verified at the serialization boundary.
func TestPushCoverageEmitsAttestationPerResource(t *testing.T) {
	t.Parallel()

	fake := newFakeVanta(t)
	s := newSink(fake.server.Client(), fake.server.URL)

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
				UserEmail: "b@example.test", AgentActive: false,
				AgentAttestation: providers.AttestationUser, AgentLastSeenAt: time.Time{},
			},
		},
	}
	require.NoError(t, s.PushCoverage(t.Context(), fake.creds(), fake.settings(), snapshot))

	byID := map[string]map[string]any{}
	for _, r := range fake.resources {
		id, ok := r["uniqueId"].(string)
		require.True(t, ok)
		props, ok := r["customProperties"].(map[string]any)
		require.True(t, ok)
		byID[id] = props
	}
	require.Len(t, byID, 2)

	require.Equal(t, "device", byID["dev-attested"]["agent_attestation"])
	require.Equal(t, true, byID["dev-attested"]["agent_active"])
	require.Equal(t, "2026-07-29T09:00:00Z", byID["dev-attested"]["agent_last_seen_at"])

	require.Equal(t, "user", byID["dev-user-only"]["agent_attestation"])
	require.Equal(t, false, byID["dev-user-only"]["agent_active"])
	lastSeen, present := byID["dev-user-only"]["agent_last_seen_at"]
	require.True(t, present, "the property is always present — an omitted field is rejected at sync")
	require.Empty(t, lastSeen, "a never-seen agent sends an empty string, not a fabricated timestamp")

	for _, props := range byID {
		require.NotContains(t, props, "assigned_user_agent_active")
		require.NotContains(t, props, "assigned_user_agent_last_seen_at")
	}
}
