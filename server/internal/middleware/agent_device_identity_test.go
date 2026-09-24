package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
)

// serveAgentPoll runs one request through the request-logging middleware and
// returns the wide event it emitted, decoded. Requests are served directly
// (no network client) so tests can craft arbitrary header bytes the way a raw
// peer could.
func serveAgentPoll(t *testing.T, target string, headers map[string]string) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := NewHTTPLoggingMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var event map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &event))
	return event
}

// The whole point of the field: a fleet sharing one organization install key
// resolves to a single user and a single API key in the request log, so the
// machine has to identify itself for a per-device count to be possible.
func TestAgentDeviceIdentityRecordsReportedHeaders(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", map[string]string{
		deviceidentity.HeaderSerial:      "C02XK1ABCDEF",
		deviceidentity.HeaderHostname:    "dev-macbook-pro",
		deviceidentity.HeaderEnvironment: "ephemeral",
	})

	// Lowercased to match the form the per-device heartbeat is stored and
	// compared in, so one machine cannot count as two devices.
	require.Equal(t, "c02xk1abcdef", event[string(attr.AgentDeviceSerialKey)])
	require.Equal(t, "dev-macbook-pro", event[string(attr.AgentDeviceHostnameKey)])
	require.Equal(t, "ephemeral", event[string(attr.AgentDeviceEnvironmentKey)])
}

// The device report endpoints carry the same identity as the policy poll, and
// are logged the same way.
func TestAgentDeviceIdentityCoversReportEndpoints(t *testing.T) {
	t.Parallel()

	for _, route := range []string{
		"/rpc/agent.reportAIScan",
		"/rpc/agent.reportSessionMoved",
		"/rpc/agent.createSessionHandoff",
	} {
		event := serveAgentPoll(t, route, map[string]string{
			deviceidentity.HeaderSerial: "fvfx1234abcd",
		})
		require.Equal(t, "fvfx1234abcd", event[string(attr.AgentDeviceSerialKey)], "route %s", route)
	}
}

// Agents predating the headers send none of them. An absent value must stay
// absent rather than becoming an empty string, or every unidentifiable device
// joins one bucket that COUNT(DISTINCT serial) counts as a single machine.
func TestAgentDeviceIdentityOmitsUnreportedValues(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", nil)

	require.NotContains(t, event, string(attr.AgentDeviceSerialKey))
	require.NotContains(t, event, string(attr.AgentDeviceHostnameKey))
	require.NotContains(t, event, string(attr.AgentDeviceEnvironmentKey))
}

// An empty header is the same as no header: the agent omits what it cannot
// read, and a proxy that materializes the header empty must not create an
// identity.
func TestAgentDeviceIdentityOmitsEmptyHeaders(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", map[string]string{
		deviceidentity.HeaderSerial:      "",
		deviceidentity.HeaderHostname:    "   ",
		deviceidentity.HeaderEnvironment: "",
	})

	require.NotContains(t, event, string(attr.AgentDeviceSerialKey))
	require.NotContains(t, event, string(attr.AgentDeviceHostnameKey))
	require.NotContains(t, event, string(attr.AgentDeviceEnvironmentKey))
}

// An SMBIOS placeholder is reported identically by many distinct machines, so
// recording it would report a whole fleet of white-box PCs as one device. The
// rest of the identity still lands.
func TestAgentDeviceIdentityDropsPlaceholderSerial(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", map[string]string{
		deviceidentity.HeaderSerial:   "To be filled by O.E.M.",
		deviceidentity.HeaderHostname: "build-box",
	})

	require.NotContains(t, event, string(attr.AgentDeviceSerialKey))
	require.Equal(t, "build-box", event[string(attr.AgentDeviceHostnameKey)])
}

// A kind this server does not recognize is served as an endpoint rather than
// rejected, and the log records where the request actually landed.
func TestAgentDeviceIdentityRecordsNormalizedEnvironment(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", map[string]string{
		deviceidentity.HeaderEnvironment: "kiosk",
	})

	require.Equal(t, deviceidentity.EnvironmentEndpoint, event[string(attr.AgentDeviceEnvironmentKey)])
}

// Header values are device-supplied input reaching a log line, so they are
// bounded and control characters are refused outright.
func TestAgentDeviceIdentitySanitizesUntrustedValues(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/agent.getPlugins", map[string]string{
		deviceidentity.HeaderSerial:   strings.Repeat("a", 200),
		deviceidentity.HeaderHostname: "dev\x01box",
	})

	require.Len(t, event[string(attr.AgentDeviceSerialKey)], 64)
	require.NotContains(t, event, string(attr.AgentDeviceHostnameKey))
}

// The headers describe a machine running the device agent. Anywhere else they
// are unverified noise, and letting them through would put requests that are
// not agent polls into the device counts taken from this field.
func TestAgentDeviceIdentityIgnoresNonAgentRoutes(t *testing.T) {
	t.Parallel()

	event := serveAgentPoll(t, "/rpc/auth.info", map[string]string{
		deviceidentity.HeaderSerial: "c02xk1abcdef",
	})

	require.NotContains(t, event, string(attr.AgentDeviceSerialKey))
}
