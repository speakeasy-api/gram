package jamf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
)

// fakeJamf is an httptest stand-in for a Jamf Pro tenant: an OAuth token
// endpoint plus a paginated computers-inventory endpoint, with knobs for
// auth failures and token expiry.
type fakeJamf struct {
	t *testing.T

	clientID     string
	clientSecret string

	devices []map[string]any

	tokenTTLSeconds int
	tokenRequests   atomic.Int32
	// expireTokensAfter invalidates all previously issued tokens once the
	// inventory endpoint has served this many requests (0 = never), forcing
	// the mid-pagination re-auth path.
	expireTokensAfter int32
	inventoryRequests atomic.Int32
	tokenGeneration   atomic.Int32

	server *httptest.Server
}

func newFakeJamf(t *testing.T, deviceCount int) *fakeJamf {
	t.Helper()
	f := &fakeJamf{
		t:                 t,
		clientID:          "test-client-id",
		clientSecret:      "test-client-secret",
		devices:           nil,
		tokenTTLSeconds:   300,
		tokenRequests:     atomic.Int32{},
		expireTokensAfter: 0,
		inventoryRequests: atomic.Int32{},
		tokenGeneration:   atomic.Int32{},
		server:            nil,
	}
	for i := range deviceCount {
		f.devices = append(f.devices, map[string]any{
			"id": strconv.Itoa(i + 1),
			"general": map[string]any{
				"name":            fmt.Sprintf("mac-%03d", i+1),
				"platform":        "Mac",
				"lastContactTime": "2026-07-27T10:00:00.500Z",
			},
			"hardware":        map[string]any{"serialNumber": fmt.Sprintf("SER%03d", i+1)},
			"operatingSystem": map[string]any{"version": "15.1"},
			"userAndLocation": map[string]any{"email": fmt.Sprintf("user%d@example.test", i+1)},
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"), "every request must carry the partner User-Agent")
		assert.NoError(t, r.ParseForm())
		if r.PostForm.Get("client_id") != f.clientID || r.PostForm.Get("client_secret") != f.clientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.tokenRequests.Add(1)
		gen := f.tokenGeneration.Load()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-gen-%d", gen),
			"expires_in":   f.tokenTTLSeconds,
		})
	})
	mux.HandleFunc("/api/v1/computers-inventory", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"), "every request must carry the partner User-Agent")
		assert.Equal(t, "id:asc", r.URL.Query().Get("sort"), "pagination must be stably ordered")
		assert.ElementsMatch(t, []string{"GENERAL", "HARDWARE", "OPERATING_SYSTEM", "USER_AND_LOCATION"}, r.URL.Query()["section"], "only mapped sections are requested")

		n := f.inventoryRequests.Add(1)
		if f.expireTokensAfter > 0 && n == f.expireTokensAfter+1 {
			// All previously issued tokens just "expired".
			f.tokenGeneration.Add(1)
		}
		expected := fmt.Sprintf("Bearer token-gen-%d", f.tokenGeneration.Load())
		if r.Header.Get("Authorization") != expected {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		size, _ := strconv.Atoi(r.URL.Query().Get("page-size"))
		start := page * size
		end := min(start+size, len(f.devices))
		results := []map[string]any{}
		if start < len(f.devices) {
			results = f.devices[start:end]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalCount": len(f.devices),
			"results":    results,
		})
	})
	f.server = httptest.NewTLSServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeJamf) creds() providers.Credentials {
	return providers.Credentials{fieldClientID: f.clientID, fieldClientSecret: f.clientSecret}
}

func (f *fakeJamf) settings() providers.Settings {
	return providers.Settings{fieldInstanceURL: f.server.URL}
}

// newSource returns a source wired to the fake's TLS client (unit tests use
// the httptest client directly; production wiring goes through guardian).
func (f *fakeJamf) newSource() *source {
	return &source{client: f.server.Client(), mu: sync.Mutex{}, token: "", tokenExpiry: time.Time{}}
}

func listAll(t *testing.T, s providers.InventorySource, creds providers.Credentials, settings providers.Settings) []providers.Device {
	t.Helper()
	var all []providers.Device
	cursor := ""
	for {
		page, err := s.ListDevices(context.Background(), creds, settings, cursor)
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

	fake := newFakeJamf(t, 205) // three pages at size 100
	s := fake.newSource()

	devices := listAll(t, s, fake.creds(), fake.settings())
	require.Len(t, devices, 205)

	first := devices[0]
	require.Equal(t, "1", first.ExternalID)
	require.Equal(t, "SER001", first.SerialNumber)
	require.Equal(t, "mac-001", first.Hostname)
	require.Equal(t, "Mac", first.OSName)
	require.Equal(t, "15.1", first.OSVersion)
	require.Equal(t, "user1@example.test", first.UserEmail)
	require.Equal(t, time.Date(2026, 7, 27, 10, 0, 0, 500000000, time.UTC), first.LastCheckInAt)
	require.NotEmpty(t, first.Raw, "the full vendor record is preserved")

	// One token mint serves the whole pull: never per request.
	require.Equal(t, int32(1), fake.tokenRequests.Load())
}

func TestTestConnection(t *testing.T) {
	t.Parallel()

	fake := newFakeJamf(t, 1)
	require.NoError(t, fake.newSource().TestConnection(context.Background(), fake.creds(), fake.settings()))

	bad := providers.Credentials{fieldClientID: "wrong", fieldClientSecret: "wrong-secret"}
	err := fake.newSource().TestConnection(context.Background(), bad, fake.settings())
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err), "credential rejections classify as auth errors")
}

func TestMidPaginationTokenExpiryClassifiesAsAuthAndDropsCache(t *testing.T) {
	t.Parallel()

	fake := newFakeJamf(t, 250)
	fake.expireTokensAfter = 1 // token dies after the first inventory page
	s := fake.newSource()

	_, err := s.ListDevices(context.Background(), fake.creds(), fake.settings(), "")
	require.NoError(t, err, "first page succeeds on the original token")

	_, err = s.ListDevices(context.Background(), fake.creds(), fake.settings(), "1")
	require.Error(t, err)
	require.True(t, providers.IsAuthError(err))

	// The cache was dropped, so the next call re-mints and succeeds — the
	// sync scheduler's retry recovers without operator action.
	page, err := s.ListDevices(context.Background(), fake.creds(), fake.settings(), "1")
	require.NoError(t, err)
	require.NotEmpty(t, page.Devices)
	require.Equal(t, int32(2), fake.tokenRequests.Load(), "exactly one re-mint after expiry")
}

func TestInstanceURLValidation(t *testing.T) {
	t.Parallel()

	fake := newFakeJamf(t, 1)
	s := fake.newSource()

	// Explicit assertions on representative cases:
	_, err := s.ListDevices(context.Background(), fake.creds(), providers.Settings{fieldInstanceURL: "http://tenant.jamfcloud.com"}, "")
	require.ErrorContains(t, err, "https")
	_, err = s.ListDevices(context.Background(), fake.creds(), providers.Settings{fieldInstanceURL: "https://tenant.jamfcloud.com/some/path"}, "")
	require.ErrorContains(t, err, "tenant root")
}

func TestInvalidCursorRejected(t *testing.T) {
	t.Parallel()

	fake := newFakeJamf(t, 1)
	_, err := fake.newSource().ListDevices(context.Background(), fake.creds(), fake.settings(), "not-a-page")
	require.ErrorContains(t, err, "cursor")
}

func TestDescriptorRegistered(t *testing.T) {
	t.Parallel()

	desc, ok := providers.Lookup(ProviderID)
	require.True(t, ok, "jamf registers on package import")
	require.Equal(t, "Jamf Pro", desc.DisplayName)
	require.Len(t, desc.Schedules, 1)
	require.Equal(t, providers.CapabilityInventorySource, desc.Schedules[0].Capability)

	secrets := desc.SecretFields()
	require.Len(t, secrets, 2, "client id and secret are write-only")
}
