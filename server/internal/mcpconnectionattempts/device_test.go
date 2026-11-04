package mcpconnectionattempts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// testDeviceKeys stands in for the keyring: one key per secret, info and organization.
func testDeviceKeys(secret string) DeriveKey {
	return func(info, organizationID string) ([]byte, error) {
		return []byte(secret + "|" + info + "|" + organizationID), nil
	}
}

const testDeviceSecret = "test-device-key-secret"

func requestFrom(forwardedFor ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.0.9:443"
	for _, value := range forwardedFor {
		req.Header.Add("X-Forwarded-For", value)
	}
	return req
}

func TestConnectionIP_OnlyTheHopOurIngressAppended(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"ingress hop", []string{"203.0.113.7"}, "203.0.113.7"},
		{"forged hops before it are ignored", []string{"198.51.100.1, 192.0.2.2, 203.0.113.7"}, "203.0.113.7"},
		{"forged header line before it is ignored", []string{"198.51.100.1", "203.0.113.7"}, "203.0.113.7"},
		{"surrounding space", []string{"198.51.100.1,  203.0.113.7 "}, "203.0.113.7"},
		{"ipv6", []string{"2001:db8::1"}, "2001:db8::1"},
		{"ipv4 mapped reads as ipv4", []string{"::ffff:203.0.113.7"}, "203.0.113.7"},
		{"no header is unsure", nil, ""},
		{"garbage is unsure", []string{"203.0.113.7, not-an-ip"}, ""},
		{"empty last hop is unsure", []string{"203.0.113.7,"}, ""},
		{"caller text is unsure", []string{"203.0.113.7:443"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, connectionIP(lastForwardedFor(requestFrom(tc.lines...))))
		})
	}
}

// RemoteAddr is the ingress, so it never stands in for the client.
func TestConnectionIP_NeverFallsBackToTheConnection(t *testing.T) {
	t.Parallel()
	require.Empty(t, connectionIP(lastForwardedFor(requestFrom())))
}

// Private network traffic doesn't pass through the ingress that overwrites the header.
func TestConnectionIP_PrivateNetworkIsUnsure(t *testing.T) {
	t.Parallel()

	req := requestFrom("203.0.113.7")
	req = req.WithContext(requestorigin.WithContext(req.Context(), requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "", OrganizationID: ""}))
	require.Empty(t, connectionIP(lastForwardedFor(req)))
}

func stampedDeviceKey(t *testing.T, secret, organizationID string, r *http.Request) string {
	t.Helper()

	row, err := RowAt(Begin(ServerScope(organizationID, uuid.New(), uuid.New(), AppDCR)), StepMCPRequest, ResultAllowed, false, time.Now(), FromRequest(r))
	require.NoError(t, err)
	recorder := &Recorder{}
	if secret != "" {
		recorder.deviceKeys = testDeviceKeys(secret)
	}
	recorder.stampClient(&row)
	require.False(t, row.client.present, "the request's values don't outlive the writer")
	return row.DeviceKey
}

func TestDeviceKey_IsStablePerOrganizationAndHidesTheAddress(t *testing.T) {
	t.Parallel()

	key := stampedDeviceKey(t, testDeviceSecret, "org-a", requestFrom("203.0.113.7"))
	require.Len(t, key, deviceKeyBytes*2)
	require.NotContains(t, key, "203")
	require.Equal(t, key, stampedDeviceKey(t, testDeviceSecret, "org-a", requestFrom("198.51.100.1, 203.0.113.7")), "a forged hop can't change it")
	require.NotEqual(t, key, stampedDeviceKey(t, testDeviceSecret, "org-b", requestFrom("203.0.113.7")), "a device can't be linked across organizations")
	require.NotEqual(t, key, stampedDeviceKey(t, testDeviceSecret, "org-a", requestFrom("203.0.113.8")))
	require.NotEqual(t, key, stampedDeviceKey(t, "other-secret", "org-a", requestFrom("203.0.113.7")))
}

func TestDeviceKey_EmptyWithNoSecretOrNoAddress(t *testing.T) {
	t.Parallel()

	require.Empty(t, stampedDeviceKey(t, "", "org-a", requestFrom("203.0.113.7")))
	require.Empty(t, stampedDeviceKey(t, testDeviceSecret, "org-a", requestFrom()))
	require.Empty(t, stampedDeviceKey(t, testDeviceSecret, "org-a", nil))
}

// A real row keeps the key on events and nowhere else, and never the address.
func TestDeviceKey_LandsOnEventsOnly(t *testing.T) {
	t.Parallel()

	conn := clickhouseConn(t)
	recorder := NewRecorder(testenv.NewLogger(t), sdkmetric.NewMeterProvider(), conn, logsOn, WithDeviceKeys(testDeviceKeys(testDeviceSecret)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(ctx)
	})
	attempt := Begin(testScope())
	req := requestFrom("203.0.113.7")
	req.Header.Set("User-Agent", "claude-code/2.0.14 (cli)")

	recorder.End(t.Context(), attempt, StepInboundConsent, CodeCanceled, FromRequest(req))
	require.NoError(t, recorder.Shutdown(t.Context()))

	var family, version, deviceKey string
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT client_family, client_version, device_key FROM mcp_connection_events WHERE attempt_id = ?`, attempt.ID()).Scan(&family, &version, &deviceKey))
	require.Equal(t, FamilyClaudeCode, family)
	require.Equal(t, "2.0.14", version)
	require.Len(t, deviceKey, deviceKeyBytes*2)

	var family2 string
	var repeats uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT client_family, sum(repeats) FROM mcp_connection_attempt_summaries WHERE organization_id = ? AND server_id = ? GROUP BY client_family`, attempt.scope.organizationID, attempt.scope.serverID.String()).Scan(&family2, &repeats))
	require.Equal(t, FamilyClaudeCode, family2)

	var columns uint64
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count() FROM system.columns WHERE database = currentDatabase() AND name = 'device_key' AND table IN ('mcp_connection_attempt_summaries', 'mcp_connection_attempt_summaries_mv', 'mcp_connection_outcomes_daily', 'mcp_connection_outcomes_daily_staging')`).Scan(&columns))
	require.Zero(t, columns, "the key stays on the 30-day events table")
}

// An organization with logs off is not hashed: its rows stop at the check before the hash.
func TestDeviceKey_LogsOffHashesNothing(t *testing.T) {
	t.Parallel()

	captured := 0
	recorder := NewCapturing(func(Row) { captured++ }, func(context.Context, string) (bool, error) { return false, nil })
	derived := 0
	recorder.deviceKeys = func(string, string) ([]byte, error) {
		derived++
		return nil, errors.New("no key")
	}
	recorder.End(t.Context(), Begin(testScope()), StepInboundConsent, CodeCanceled, FromRequest(requestFrom("203.0.113.7")))
	require.Zero(t, captured)
	require.Zero(t, derived, "no key is derived for an organization with logs off")
}

// An unusable keyring leaves the key empty and the row intact.
func TestDeviceKey_UnusableKeyLeavesItEmpty(t *testing.T) {
	t.Parallel()

	recorder := &Recorder{deviceKeys: func(string, string) ([]byte, error) { return nil, errors.New("no keyring") }}
	require.Empty(t, recorder.deviceKeyFor("org-a", "203.0.113.7"))
}

func TestDeviceKey_AsksForItsOwnPurpose(t *testing.T) {
	t.Parallel()

	var infos []string
	recorder := &Recorder{deviceKeys: func(info, _ string) ([]byte, error) {
		infos = append(infos, info)
		return []byte("key"), nil
	}}
	require.NotEmpty(t, recorder.deviceKeyFor("org-a", "203.0.113.7"))
	require.Equal(t, []string{"gram/connection/device"}, infos)
}
