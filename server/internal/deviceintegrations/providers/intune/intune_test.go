package intune

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
)

const testTenantID = "00000000-0000-0000-0000-000000000001"

// fakeGraph is an httptest stand-in for Entra + Microsoft Graph: a
// client-credentials token endpoint with Entra's 400/invalid_client error
// shape, and a managedDevices listing paged via @odata.nextLink.
type fakeGraph struct {
	mu           sync.Mutex
	clientID     string
	clientSecret string
	tokenMints   int
	// expiresIn is the token lifetime the fake reports.
	expiresIn int
	// rejectDevices makes the devices endpoint 401 every request,
	// simulating a revoked Graph permission.
	rejectDevices bool
	// serverPageSize is the server-driven page size (the client sends no
	// $top, exactly like production Intune).
	serverPageSize int
	devices        []map[string]any

	server *httptest.Server
}

func newFakeGraph(t *testing.T, deviceCount int) *fakeGraph {
	t.Helper()
	f := &fakeGraph{
		mu:             sync.Mutex{},
		clientID:       "test-client-id",
		clientSecret:   "test-client-secret",
		tokenMints:     0,
		expiresIn:      3599,
		rejectDevices:  false,
		serverPageSize: 1000,
		devices:        nil,
		server:         nil,
	}
	for i := range deviceCount {
		f.devices = append(f.devices, map[string]any{
			"id":                fmt.Sprintf("guid-%04d", i+1),
			"deviceName":        fmt.Sprintf("pc-%04d", i+1),
			"serialNumber":      fmt.Sprintf("SER%04d", i+1),
			"operatingSystem":   "Windows",
			"osVersion":         "10.0.26100",
			"userPrincipalName": fmt.Sprintf("user%d@example.test", i+1),
			"emailAddress":      "",
			"lastSyncDateTime":  "2026-07-28T10:00:00.5000000Z",
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+testTenantID+"/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"), "every request carries the integration User-Agent")
		assert.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		assert.Contains(t, r.PostForm.Get("scope"), "/.default", "the Graph default scope is requested")

		f.mu.Lock()
		defer f.mu.Unlock()
		if r.PostForm.Get("client_id") != f.clientID || r.PostForm.Get("client_secret") != f.clientSecret {
			// Entra's real shape: HTTP 400 with a JSON error code, NOT 401.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":             "invalid_client",
				"error_description": "AADSTS7000215: Invalid client secret provided.",
			})
			return
		}
		f.tokenMints++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("graph-token-%d", f.tokenMints),
			"expires_in":   f.expiresIn,
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/v1.0/deviceManagement/managedDevices", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
		assert.NotEmpty(t, r.URL.Query().Get("$select"), "pages are field-selected")
		assert.Empty(t, r.URL.Query().Get("$top"), "page size is server-driven — Intune's OData subset makes $top unsafe")
		f.mu.Lock()
		expected := fmt.Sprintf("Bearer graph-token-%d", f.tokenMints)
		reject := f.rejectDevices
		f.mu.Unlock()
		if reject || r.Header.Get("Authorization") != expected {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		skip := 0
		if raw := r.URL.Query().Get("$skiptoken"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			assert.NoError(t, err)
			skip = parsed
		}

		f.mu.Lock()
		end := min(skip+f.serverPageSize, len(f.devices))
		page := f.devices[skip:end]
		total := len(f.devices)
		f.mu.Unlock()

		response := map[string]any{"value": page}
		if end < total {
			// Graph-style opaque continuation: an absolute URL on this host.
			response["@odata.nextLink"] = f.server.URL + "/v1.0/deviceManagement/managedDevices?$select=x&$skiptoken=" + strconv.Itoa(end)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGraph) creds() providers.Credentials {
	return providers.Credentials{fieldClientID: f.clientID, fieldClientSecret: f.clientSecret}
}

func (f *fakeGraph) settings() providers.Settings {
	return providers.Settings{fieldTenantID: testTenantID}
}

// newSource wires a source to the fake for both hosts (unit tests use the
// httptest client directly; production wiring goes through guardian).
func (f *fakeGraph) newSource() *source {
	return newSource(f.server.Client(), f.server.URL, f.server.URL)
}

func listAll(t *testing.T, s providers.InventorySource, creds providers.Credentials, settings providers.Settings) []providers.Device {
	t.Helper()
	var all []providers.Device
	cursor := ""
	for {
		page, err := s.ListDevices(t.Context(), creds, settings, cursor)
		require.NoError(t, err)
		all = append(all, page.Devices...)
		if page.NextCursor == "" {
			return all
		}
		cursor = page.NextCursor
	}
}

func TestListDevicesPaginatesAndMaps(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 2500) // three server-driven pages at 1000
	s := fake.newSource()

	devices := listAll(t, s, fake.creds(), fake.settings())
	require.Len(t, devices, 2500)

	first := devices[0]
	require.Equal(t, "guid-0001", first.ExternalID)
	require.Equal(t, "SER0001", first.SerialNumber)
	require.Equal(t, "pc-0001", first.Hostname)
	require.Equal(t, "Windows", first.OSName)
	require.Equal(t, "10.0.26100", first.OSVersion)
	require.Equal(t, "user1@example.test", first.UserEmail, "UPN attributes the device when emailAddress is empty")
	require.Equal(t, time.Date(2026, 7, 28, 10, 0, 0, 500000000, time.UTC), first.LastCheckInAt)
	require.NotEmpty(t, first.Raw, "the full vendor record is preserved")

	// One token mint serves the whole pull, and no page repeats a device.
	require.Equal(t, 1, fake.tokenMints)
	seen := make(map[string]bool, len(devices))
	for _, d := range devices {
		require.False(t, seen[d.ExternalID], "device %s repeated across pages", d.ExternalID)
		seen[d.ExternalID] = true
	}
}

func TestEmailAddressPreferredOverUPN(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	fake.mu.Lock()
	fake.devices[0]["emailAddress"] = "mailbox@example.test"
	fake.mu.Unlock()

	devices := listAll(t, fake.newSource(), fake.creds(), fake.settings())
	require.Len(t, devices, 1)
	require.Equal(t, "mailbox@example.test", devices[0].UserEmail)
}

func TestTestConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	require.NoError(t, fake.newSource().TestConnection(t.Context(), fake.creds(), fake.settings()))

	// Entra rejects bad client credentials as 400 invalid_client — which
	// must classify as a credential rejection, or a revoked secret would
	// retry forever instead of auto-pausing.
	bad := providers.Credentials{fieldClientID: "wrong", fieldClientSecret: "wrong-secret"}
	err := fake.newSource().TestConnection(t.Context(), bad, fake.settings())
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err), "Entra 400/invalid_client classifies as an auth error")
	require.ErrorContains(t, err, "invalid_client")
}

func TestTenantIDValidation(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	s := fake.newSource()

	_, err := s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldTenantID: ""}, "")
	require.ErrorContains(t, err, "not configured")
	_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldTenantID: "bad/tenant?x"}, "")
	require.ErrorContains(t, err, "directory id")
}

func TestCursorMustStayOnGraphHost(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	s := fake.newSource()

	// The cursor round-trips through the framework as an opaque string; a
	// crafted cursor must not send the bearer token to an arbitrary host.
	_, err := s.ListDevices(t.Context(), fake.creds(), fake.settings(), "https://evil.example.test/v1.0/deviceManagement/managedDevices")
	require.ErrorContains(t, err, "Graph host")
	_, err = s.ListDevices(t.Context(), fake.creds(), fake.settings(), "::not a url")
	require.ErrorContains(t, err, "cursor")
}

func TestMissingDeviceIDFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 0)
	fake.mu.Lock()
	fake.devices = []map[string]any{{"id": "", "deviceName": "ghost", "operatingSystem": "Windows"}}
	fake.mu.Unlock()

	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "no id")
}

func TestUnparseableLastSyncFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 0)
	fake.mu.Lock()
	fake.devices = []map[string]any{{"id": "guid-a", "deviceName": "a", "operatingSystem": "Windows", "lastSyncDateTime": "07/28/2026 10:00"}}
	fake.mu.Unlock()

	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "lastSyncDateTime", "a format drift must fail loudly, not silently NULL stored check-ins")
}

func TestMidPullTokenExpiryRecoversInRun(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	s := fake.newSource()

	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))

	// The active token is invalidated (expiry crossing during a slow
	// request, or a rotation): one re-mint retry absorbs it in-run, so no
	// spurious credential rejection reaches the scheduler.
	fake.mu.Lock()
	fake.tokenMints++ // the fake now expects graph-token-N+1
	fake.mu.Unlock()
	devices := listAll(t, s, fake.creds(), fake.settings())
	require.Len(t, devices, 1)
}

func TestPersistentRejectionClassifiesAsAuth(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	s := fake.newSource()

	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))

	// A revoked Graph permission rejects both the cached token and the
	// re-minted one: after the single in-run retry, the failure surfaces
	// as the auth error it is.
	fake.mu.Lock()
	fake.rejectDevices = true
	fake.mu.Unlock()
	_, err := s.ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err))
}

func TestShortTokenLifetimeStillCaches(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	fake.mu.Lock()
	fake.expiresIn = 20 // below 2x the 30s slack: the ttl/2 floor must hold
	fake.mu.Unlock()
	s := fake.newSource()

	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))
	require.NoError(t, s.TestConnection(t.Context(), fake.creds(), fake.settings()))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	require.Equal(t, 1, fake.tokenMints, "one mint serves back-to-back probes even under a short lifetime")
}

func TestTenantAliasesRejected(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	s := fake.newSource()

	for _, alias := range []string{"common", "organizations", "consumers", "..", "."} {
		_, err := s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldTenantID: alias}, "")
		require.ErrorContains(t, err, "directory id", "alias %q must be rejected with the friendly message", alias)
	}
}

func TestCursorHostToleratesCaseAndDefaultPort(t *testing.T) {
	t.Parallel()

	fake := newFakeGraph(t, 1)
	// A source pointed at a cased/ported Graph base must accept the
	// semantically identical nextLink variants Microsoft could emit, and
	// still refuse a genuinely different host.
	s := newSource(fake.server.Client(), fake.server.URL, "https://Graph.Example.Test")

	for _, ok := range []string{
		"https://graph.example.test/v1.0/deviceManagement/managedDevices?$skiptoken=1",
		"https://GRAPH.EXAMPLE.TEST/v1.0/deviceManagement/managedDevices?$skiptoken=1",
		"https://graph.example.test:443/v1.0/deviceManagement/managedDevices?$skiptoken=1",
	} {
		_, err := s.pageURL(ok)
		require.NoError(t, err, "cursor %q is the Graph host", ok)
	}
	_, err := s.pageURL("https://evil.example.test/v1.0/deviceManagement/managedDevices")
	require.ErrorContains(t, err, "Graph host")
	_, err = s.pageURL("http://graph.example.test/v1.0/deviceManagement/managedDevices")
	require.ErrorContains(t, err, "Graph host", "https is required")
}

func TestDescriptorRegistered(t *testing.T) {
	t.Parallel()

	desc, ok := providers.Lookup(ProviderID)
	require.True(t, ok)
	require.True(t, desc.HasCapability(providers.CapabilityInventorySource))

	secretKeys := make([]string, 0, 2)
	for _, f := range desc.SecretFields() {
		secretKeys = append(secretKeys, f.Key)
	}
	require.ElementsMatch(t, []string{fieldClientID, fieldClientSecret}, secretKeys,
		"the app credential pair is write-only; the tenant id stays readable")
	require.Len(t, desc.Schedules, 1)
	require.Equal(t, providers.CapabilityInventorySource, desc.Schedules[0].Capability)
}
