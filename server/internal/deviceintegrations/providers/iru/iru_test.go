package iru

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
)

// fakeIru is an httptest stand-in for an Iru tenant: a bearer-authed
// devices endpoint with limit/offset pagination and the vendor's
// empty-string-user quirk.
type fakeIru struct {
	t *testing.T

	apiToken string

	// devMu guards devices so tests can mutate the fleet between requests
	// without racing the handler goroutine.
	devMu   sync.Mutex
	devices []map[string]any

	server *httptest.Server
}

func newFakeIru(t *testing.T, deviceCount int) *fakeIru {
	t.Helper()
	f := &fakeIru{
		t:        t,
		apiToken: "test-api-token",
		devMu:    sync.Mutex{},
		devices:  nil,
		server:   nil,
	}
	for i := range deviceCount {
		f.devices = append(f.devices, map[string]any{
			"device_id":     fmt.Sprintf("uuid-%04d", i+1),
			"device_name":   fmt.Sprintf("mac-%04d", i+1),
			"serial_number": fmt.Sprintf("SER%04d", i+1),
			"platform":      "Mac",
			"os_version":    "15.1",
			"user": map[string]any{
				"email": fmt.Sprintf("user%d@example.test", i+1),
				"name":  fmt.Sprintf("User %d", i+1),
			},
			"last_check_in": "2026-07-27T10:00:00.500000+00:00",
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"), "every request carries the integration User-Agent")
		if r.Header.Get("Authorization") != "Bearer "+f.apiToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		assert.NoError(t, err, "limit must always be set")
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		assert.NoError(t, err, "offset must always be set")

		f.devMu.Lock()
		results := []map[string]any{}
		for i := offset; i < len(f.devices) && len(results) < limit; i++ {
			results = append(results, f.devices[i])
		}
		f.devMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		// The real API returns a bare array, not an envelope.
		_ = json.NewEncoder(w).Encode(results)
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeIru) creds() providers.Credentials {
	return providers.Credentials{fieldAPIToken: f.apiToken}
}

func (f *fakeIru) settings() providers.Settings {
	return providers.Settings{fieldInstanceURL: f.server.URL}
}

// newSource returns a source wired to the fake's TLS client (unit tests use
// the httptest client directly; production wiring goes through guardian).
func (f *fakeIru) newSource() *source {
	return &source{client: f.server.Client()}
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

	fake := newFakeIru(t, 650) // three pages at size 300
	s := fake.newSource()

	devices := listAll(t, s, fake.creds(), fake.settings())
	require.Len(t, devices, 650)

	first := devices[0]
	require.Equal(t, "uuid-0001", first.ExternalID)
	require.Equal(t, "SER0001", first.SerialNumber)
	require.Equal(t, "mac-0001", first.Hostname)
	require.Equal(t, "Mac", first.OSName)
	require.Equal(t, "15.1", first.OSVersion)
	require.Equal(t, "user1@example.test", first.UserEmail)
	require.Equal(t, time.Date(2026, 7, 27, 10, 0, 0, 500000000, time.UTC), first.LastCheckInAt)
	require.NotEmpty(t, first.Raw, "the full vendor record is preserved")

	// No repeats across page boundaries on a stable fleet.
	seen := make(map[string]bool, len(devices))
	for _, d := range devices {
		require.False(t, seen[d.ExternalID], "device %s repeated across pages", d.ExternalID)
		seen[d.ExternalID] = true
	}
}

func TestUnassignedUserRepresentations(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 0)
	fake.devices = []map[string]any{
		// The API's unassigned-device quirk: user is an empty string.
		{"device_id": "uuid-a", "device_name": "a", "serial_number": "SA", "platform": "Mac", "os_version": "15.1", "user": "", "last_check_in": ""},
		{"device_id": "uuid-b", "device_name": "b", "serial_number": "SB", "platform": "Mac", "os_version": "15.1", "user": nil, "last_check_in": ""},
		{"device_id": "uuid-c", "device_name": "c", "serial_number": "SC", "platform": "Mac", "os_version": "15.1"},
	}

	devices := listAll(t, fake.newSource(), fake.creds(), fake.settings())
	require.Len(t, devices, 3)
	for _, d := range devices {
		require.Empty(t, d.UserEmail, "device %s must map to no assigned user", d.ExternalID)
		require.True(t, d.LastCheckInAt.IsZero(), "device %s has no check-in time", d.ExternalID)
	}
}

func TestTestConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 1)
	require.NoError(t, fake.newSource().TestConnection(t.Context(), fake.creds(), fake.settings()))

	bad := providers.Credentials{fieldAPIToken: "wrong-token"}
	err := fake.newSource().TestConnection(t.Context(), bad, fake.settings())
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err), "credential rejections classify as auth errors")
}

func TestAuthRejectionMidPullClassifiesAsAuth(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 650)
	s := fake.newSource()
	staleCreds := fake.creds()

	first, err := s.ListDevices(t.Context(), staleCreds, fake.settings(), "")
	require.NoError(t, err)
	require.NotEmpty(t, first.NextCursor)

	// The tenant rotates the token between pages: our stored credential is
	// now stale, and the failure must classify as an auth error so the sync
	// runner counts it toward auto-pause.
	fake.apiToken = "rotated-away"
	_, err = s.ListDevices(t.Context(), staleCreds, fake.settings(), first.NextCursor)
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err))
}

func TestMissingDeviceIDFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 0)
	fake.devices = []map[string]any{
		{"device_id": "", "device_name": "ghost", "serial_number": "SG", "platform": "Mac", "os_version": "15.1"},
	}

	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "device_id")
}

func TestReadBoundedBodyDetectsTruncation(t *testing.T) {
	t.Parallel()

	body, err := readBoundedBody(strings.NewReader("small"))
	require.NoError(t, err)
	require.Equal(t, "small", string(body))

	_, err = readBoundedBody(strings.NewReader(strings.Repeat("x", maxResponseBytes+1)))
	require.ErrorContains(t, err, "limit", "an oversized body must fail loudly, not decode a truncated payload")
}

func TestInstanceURLValidation(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 1)
	s := fake.newSource()

	_, err := s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: "http://tenant.api.kandji.io"}, "")
	require.ErrorContains(t, err, "https")
	_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: "https://tenant.api.kandji.io/some/path"}, "")
	require.ErrorContains(t, err, "tenant API root")
	_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: ""}, "")
	require.ErrorContains(t, err, "not configured")
}

func TestInvalidCursorRejected(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 1)
	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "not-an-offset")
	require.ErrorContains(t, err, "cursor")
	_, err = fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "-5")
	require.ErrorContains(t, err, "cursor")
}
