package iru

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

// fakeIru is an httptest stand-in for an Iru tenant: a bearer-authed
// devices endpoint with limit/offset pagination and the vendor's
// empty-string-user quirk.
type fakeIru struct {
	// mu guards every field the handler goroutine shares with the test
	// goroutine (the fleet and the accepted token); tests mutate them only
	// through the accessors below.
	mu       sync.Mutex
	apiToken string
	devices  []map[string]any

	server *httptest.Server
}

// setDevices replaces the fleet mid-test (enrollment churn).
func (f *fakeIru) setDevices(devices []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = devices
}

// removeDevice unenrolls one device mid-test.
func (f *fakeIru) removeDevice(externalID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.devices[:0]
	for _, d := range f.devices {
		if d["device_id"] != externalID {
			kept = append(kept, d)
		}
	}
	f.devices = kept
}

// setToken rotates the token the fake accepts.
func (f *fakeIru) setToken(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiToken = token
}

func newFakeIru(t *testing.T, deviceCount int) *fakeIru {
	t.Helper()
	f := &fakeIru{
		mu:       sync.Mutex{},
		apiToken: "test-api-token",
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

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		assert.NoError(t, err, "limit must always be set")
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		assert.NoError(t, err, "offset must always be set")

		f.mu.Lock()
		authorized := r.Header.Get("Authorization") == "Bearer "+f.apiToken
		results := []map[string]any{}
		for i := offset; i < len(f.devices) && len(results) < limit; i++ {
			results = append(results, f.devices[i])
		}
		f.mu.Unlock()
		if !authorized {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		// The real API returns a bare array, not an envelope.
		_ = json.NewEncoder(w).Encode(results)
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeIru) creds() providers.Credentials {
	f.mu.Lock()
	defer f.mu.Unlock()
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

	// Three full-or-partial pages at size 300, plus the final empty page
	// that terminates the listing.
	fake := newFakeIru(t, 650)
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
	fake.setDevices([]map[string]any{
		// The API's unassigned-device quirk: user is an empty string.
		{"device_id": "uuid-a", "device_name": "a", "serial_number": "SA", "platform": "Mac", "os_version": "15.1", "user": "", "last_check_in": ""},
		{"device_id": "uuid-b", "device_name": "b", "serial_number": "SB", "platform": "Mac", "os_version": "15.1", "user": nil, "last_check_in": ""},
		{"device_id": "uuid-c", "device_name": "c", "serial_number": "SC", "platform": "Mac", "os_version": "15.1"},
		// A present-but-null email is one odd user record, not schema drift:
		// it must not block the org's entire pull.
		{"device_id": "uuid-d", "device_name": "d", "serial_number": "SD", "platform": "Mac", "os_version": "15.1", "user": map[string]any{"email": nil}},
	})

	devices := listAll(t, fake.newSource(), fake.creds(), fake.settings())
	require.Len(t, devices, 4)
	for _, d := range devices {
		require.Empty(t, d.UserEmail, "device %s must map to no assigned user", d.ExternalID)
		require.True(t, d.LastCheckInAt.IsZero(), "device %s has no check-in time", d.ExternalID)
	}
}

func TestUserSchemaDriftFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 0)
	s := fake.newSource()

	// An assigned-but-undecodable user is schema drift, not "unassigned":
	// swallowing it would quietly zero the fleet's coverage attribution.
	fake.setDevices([]map[string]any{
		{"device_id": "uuid-a", "device_name": "a", "serial_number": "SA", "platform": "Mac", "os_version": "15.1", "user": 123},
	})
	_, err := s.ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "decode user")

	// A user object whose email key vanished (renamed/dropped) is the same
	// drift class and must not silently read as unassigned.
	fake.setDevices([]map[string]any{
		{"device_id": "uuid-b", "device_name": "b", "serial_number": "SB", "platform": "Mac", "os_version": "15.1", "user": map[string]any{"mail": "user@example.test"}},
	})
	_, err = s.ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "no email field")

	// A non-string email is drift too.
	fake.setDevices([]map[string]any{
		{"device_id": "uuid-c", "device_name": "c", "serial_number": "SC", "platform": "Mac", "os_version": "15.1", "user": map[string]any{"email": 123}},
	})
	_, err = s.ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "decode user email")
}

func TestUnparseableCheckInFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 0)
	fake.setDevices([]map[string]any{
		{"device_id": "uuid-a", "device_name": "a", "serial_number": "SA", "platform": "Mac", "os_version": "15.1", "user": "", "last_check_in": "07/28/2026 10:00"},
	})

	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "last_check_in", "a format drift must fail loudly, not silently NULL stored check-ins")
}

func TestMidPullDeletionSkipsAtMostBoundaryDevices(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 650)
	s := fake.newSource()

	first, err := s.ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.NoError(t, err)
	require.Len(t, first.Devices, 300)

	// A device already consumed is unenrolled mid-pull. Offset pagination
	// shifts every later row one slot down, so exactly one boundary device
	// (the old index-300 row, which slides into the already-consumed window)
	// is skipped — this test pins the documented blast radius: no
	// duplicates, and at most one omission per unenrollment.
	fake.removeDevice("uuid-0010")

	seen := make(map[string]bool, 650)
	for _, d := range first.Devices {
		seen[d.ExternalID] = true
	}
	cursor := first.NextCursor
	for cursor != "" {
		page, err := s.ListDevices(t.Context(), fake.creds(), fake.settings(), cursor)
		require.NoError(t, err)
		for _, d := range page.Devices {
			require.False(t, seen[d.ExternalID], "device %s repeated across pages", d.ExternalID)
			seen[d.ExternalID] = true
		}
		cursor = page.NextCursor
	}

	require.True(t, seen["uuid-0010"], "the unenrolled device was consumed before removal")
	require.Len(t, seen, 649, "exactly one boundary device is skipped per mid-pull unenrollment")
	require.False(t, seen["uuid-0301"], "the skipped device is the one that slid across the page boundary")
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
	fake.setToken("rotated-away")
	_, err = s.ListDevices(t.Context(), staleCreds, fake.settings(), first.NextCursor)
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err))
}

func TestMissingDeviceIDFailsLoudly(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 0)
	fake.setDevices([]map[string]any{
		{"device_id": "", "device_name": "ghost", "serial_number": "SG", "platform": "Mac", "os_version": "15.1"},
	})

	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "")
	require.ErrorContains(t, err, "device_id")
}

func TestInstanceURLValidation(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 1)
	s := fake.newSource()

	_, err := s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: "http://tenant.api.kandji.io"}, "")
	require.ErrorContains(t, err, "https")
	_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: "https://tenant.api.kandji.io/some/path"}, "")
	require.ErrorContains(t, err, "tenant API root")
	// Vendor-domain hosts that are not a tenant API root are the common
	// paste mistakes: the console URL, the bare apex, a tenant-less API
	// host, and a trailing-dot FQDN variant of any of them. All get the
	// error naming the right value.
	for _, bad := range []string{
		"https://tenant.iru.com",
		"https://tenant.kandji.io",
		"https://iru.com",
		"https://kandji.io",
		"https://api.iru.com",
		"https://tenant.iru.com.",
	} {
		_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: bad}, "")
		require.ErrorContains(t, err, "tenant API root", "value %q must be rejected", bad)
	}
	_, err = s.ListDevices(t.Context(), fake.creds(), providers.Settings{fieldInstanceURL: ""}, "")
	require.ErrorContains(t, err, "not configured")
}

func TestVendorAPIHostShapes(t *testing.T) {
	t.Parallel()

	// Valid tenant API roots, including regional forms, pass the guard.
	for _, good := range []string{
		"tenant.api.iru.com",
		"tenant.api.kandji.io",
		"tenant.api.eu.kandji.io",
	} {
		require.True(t, vendorAPIHost(good), "host %q is a tenant API root", good)
	}
	for _, bad := range []string{"tenant.iru.com", "api.iru.com", "iru.com"} {
		require.False(t, vendorAPIHost(bad), "host %q is not a tenant API root", bad)
	}
}

func TestInvalidCursorRejected(t *testing.T) {
	t.Parallel()

	fake := newFakeIru(t, 1)
	_, err := fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "not-an-offset")
	require.ErrorContains(t, err, "cursor")
	_, err = fake.newSource().ListDevices(t.Context(), fake.creds(), fake.settings(), "-5")
	require.ErrorContains(t, err, "cursor")
}
