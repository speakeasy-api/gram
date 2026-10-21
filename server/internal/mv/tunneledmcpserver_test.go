package mv

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestTunnelDiagnosticsUsesReceiptAgeAndFailureHysteresis(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	r := &wire.DiagnosticsReport{Version: 1, TargetState: "unreachable", ConsecutiveFailures: 1, SampleAgeMillis: 1000, TCP: wire.DiagnosticStep{State: "fail", Failure: "tcp_refused"}}
	status := &route.Diagnostics{State: "available", ReceivedAt: now.Add(-time.Second), Report: r}
	view := buildTunnelDiagnostics(status, now)
	require.Equal(t, "unknown", *view.TargetState)
	require.Equal(t, "tcp_refused", view.TCP.Failure)
	require.EqualValues(t, 2000, *view.SampleAgeMs)
	r.ConsecutiveFailures = 2
	require.Equal(t, "unreachable", *buildTunnelDiagnostics(status, now).TargetState)
	require.Equal(t, "stale", buildTunnelDiagnostics(status, now.Add(wire.DiagnosticsFreshness)).State)
	status.State = "unavailable"
	require.Equal(t, "unavailable", buildTunnelDiagnostics(status, now).State)
}

func TestTunnelHTTPProgressRemainsOptionalAndSaturates(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	report := &wire.DiagnosticsReport{TargetState: "reachable"}
	status := &route.Diagnostics{State: "available", ReceivedAt: now, Report: report}
	require.Nil(t, buildTunnelDiagnostics(status, now).HTTPProgress)
	report.HTTPProgress = &wire.HTTPProgress{WaitingHeaders: 2, OpenResponses: ^uint64(0)}
	view := buildTunnelDiagnostics(status, now)
	require.EqualValues(t, 2, view.HTTPProgress.WaitingHeaders)
	require.EqualValues(t, math.MaxInt64, view.HTTPProgress.OpenResponses)
	require.Equal(t, "stale", buildTunnelDiagnostics(status, now.Add(wire.DiagnosticsFreshness+time.Second)).State)
}

// tunnelDiagnosticsResult is the part of a returned connection's diagnostics
// that does not depend on the view's clock; report ages grow while it is read.
type tunnelDiagnosticsResult struct {
	State               string
	TargetState         *string
	ConsecutiveFailures *int64
	DNS                 *types.TunnelDiagnosticStep
	TCP                 *types.TunnelDiagnosticStep
	TLS                 *types.TunnelDiagnosticStep
}

func TestBuildTunneledMcpServerConnectionsView(t *testing.T) {
	t.Parallel()

	connectedAt := time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)
	heartbeatAt := connectedAt.Add(time.Minute)
	// The view reads its own clock, so reports are received now to stay fresh.
	receivedAt := time.Now().UTC()
	pass := wire.DiagnosticStep{State: "pass", DurationMillis: 4, Failure: ""}
	notTested := wire.DiagnosticStep{State: "not_tested", DurationMillis: 0, Failure: ""}
	dnsNotFound := wire.DiagnosticStep{State: "fail", DurationMillis: 12, Failure: "dns_not_found"}
	reported := func(targetState string, failures uint32, dns, tcp, tls wire.DiagnosticStep) *route.Diagnostics {
		return &route.Diagnostics{State: "available", AttemptedAt: receivedAt, ReceivedAt: receivedAt, Report: &wire.DiagnosticsReport{
			Version: 1, Sequence: 7, SampleAgeMillis: 500, TargetState: targetState, ConsecutiveFailures: failures,
			DNS: dns, TCP: tcp, TLS: tls, LastHTTPResponseAgeMillis: -1, LastTransportErrorAgeMillis: -1,
		}}
	}
	connection := func(sessionID, agentVersion, targetDisplay string, consumers int, metadata map[string]string, diagnostics *route.Diagnostics) route.Connection {
		return route.Connection{
			GatewaySessionID: sessionID, ServiceVersion: "1.4.0", AgentVersion: agentVersion,
			ConnectedAt: connectedAt, LastHeartbeatAt: heartbeatAt, RemoteAddr: "10.0.0.5:51234",
			ActiveSubstreams: 2, ActiveConsumerSessions: consumers, Metadata: metadata,
			TargetDisplay: targetDisplay, Diagnostics: diagnostics,
		}
	}
	view := func(sessionID string, agentVersion, targetDisplay *string, consumers int, metadata map[string]string) *types.TunneledMcpConnection {
		return &types.TunneledMcpConnection{
			GatewaySessionID: sessionID, ServiceVersion: "1.4.0", AgentVersion: agentVersion,
			ConnectedAt: "2026-10-21T09:30:00Z", LastHeartbeatAt: "2026-10-21T09:31:00Z", RemoteAddr: new("10.0.0.5:51234"),
			ActiveSubstreams: 2, ActiveConsumerSessions: consumers, Metadata: metadata,
			TargetDisplay: targetDisplay, Diagnostics: nil,
		}
	}
	step := func(s wire.DiagnosticStep) *types.TunnelDiagnosticStep {
		return &types.TunnelDiagnosticStep{State: s.State, DurationMs: s.DurationMillis, Failure: s.Failure}
	}
	result := func(targetState string, failures int64, dns, tcp, tls wire.DiagnosticStep) *tunnelDiagnosticsResult {
		return &tunnelDiagnosticsResult{State: "available", TargetState: new(targetState), ConsecutiveFailures: new(failures), DNS: step(dns), TCP: step(tcp), TLS: step(tls)}
	}
	stateOnly := func(state string) *tunnelDiagnosticsResult {
		return &tunnelDiagnosticsResult{State: state, TargetState: nil, ConsecutiveFailures: nil, DNS: nil, TCP: nil, TLS: nil}
	}

	reachable := connection("session-reachable", "0.9.0", "https://user:secret@mcp.internal:8443/mcp/secret-token?key=private", 2,
		map[string]string{"region": "eu-west-1", "owner": ""}, reported("reachable", 0, pass, pass, pass))
	reachableView := view("session-reachable", new("0.9.0"), new("https://mcp.internal:8443"), 2, map[string]string{"region": "eu-west-1"})
	unsupported := connection("session-unsupported", "", "", 1, nil, &route.Diagnostics{State: "unsupported"})
	unsupportedView := view("session-unsupported", nil, nil, 1, map[string]string{})
	disabled := connection("session-disabled", "0.9.0", "", 0, nil, &route.Diagnostics{State: "disabled"})
	disabledView := view("session-disabled", new("0.9.0"), nil, 0, map[string]string{})

	tests := []struct {
		name                 string
		connections          []route.Connection
		wantConnections      []*types.TunneledMcpConnection
		wantDiagnostics      []*tunnelDiagnosticsResult
		wantConsumerSessions int
	}{
		{
			name:                 "no connections",
			connections:          nil,
			wantConnections:      []*types.TunneledMcpConnection{},
			wantDiagnostics:      []*tunnelDiagnosticsResult{},
			wantConsumerSessions: 0,
		},
		{
			name:                 "reachable target with credentials and path removed from the display",
			connections:          []route.Connection{reachable},
			wantConnections:      []*types.TunneledMcpConnection{reachableView},
			wantDiagnostics:      []*tunnelDiagnosticsResult{result("reachable", 0, pass, pass, pass)},
			wantConsumerSessions: 2,
		},
		{
			name: "unreachable target after repeated DNS failures",
			connections: []route.Connection{connection("session-dns", "0.9.0", "https://mcp.internal:8443", 0, nil,
				reported("unreachable", 3, dnsNotFound, notTested, notTested))},
			wantConnections:      []*types.TunneledMcpConnection{view("session-dns", new("0.9.0"), new("https://mcp.internal:8443"), 0, map[string]string{})},
			wantDiagnostics:      []*tunnelDiagnosticsResult{result("unreachable", 3, dnsNotFound, notTested, notTested)},
			wantConsumerSessions: 0,
		},
		{
			name: "a single unreachable report is shown as unknown",
			connections: []route.Connection{connection("session-dns", "0.9.0", "https://mcp.internal:8443", 0, nil,
				reported("unreachable", 1, dnsNotFound, notTested, notTested))},
			wantConnections:      []*types.TunneledMcpConnection{view("session-dns", new("0.9.0"), new("https://mcp.internal:8443"), 0, map[string]string{})},
			wantDiagnostics:      []*tunnelDiagnosticsResult{result("unknown", 1, dnsNotFound, notTested, notTested)},
			wantConsumerSessions: 0,
		},
		{
			name:                 "agent that predates diagnostics",
			connections:          []route.Connection{unsupported},
			wantConnections:      []*types.TunneledMcpConnection{unsupportedView},
			wantDiagnostics:      []*tunnelDiagnosticsResult{stateOnly("unsupported")},
			wantConsumerSessions: 1,
		},
		{
			name:                 "agent with diagnostics turned off",
			connections:          []route.Connection{disabled},
			wantConnections:      []*types.TunneledMcpConnection{disabledView},
			wantDiagnostics:      []*tunnelDiagnosticsResult{stateOnly("disabled")},
			wantConsumerSessions: 0,
		},
		{
			name:                 "connection recorded without diagnostics",
			connections:          []route.Connection{connection("session-bare", "", "", 0, nil, nil)},
			wantConnections:      []*types.TunneledMcpConnection{view("session-bare", nil, nil, 0, map[string]string{})},
			wantDiagnostics:      []*tunnelDiagnosticsResult{nil},
			wantConsumerSessions: 0,
		},
		{
			name:                 "mixed agents keep their order and sum consumer sessions",
			connections:          []route.Connection{reachable, unsupported, disabled},
			wantConnections:      []*types.TunneledMcpConnection{reachableView, unsupportedView, disabledView},
			wantDiagnostics:      []*tunnelDiagnosticsResult{result("reachable", 0, pass, pass, pass), stateOnly("unsupported"), stateOnly("disabled")},
			wantConsumerSessions: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := BuildTunneledMcpServerConnectionsView(tc.connections)

			require.Equal(t, "available", *got.CollectionState)
			_, err := time.Parse(time.RFC3339, *got.ObservedAt)
			require.NoError(t, err)
			require.Equal(t, len(tc.wantConnections), got.ActiveConnectionCount)
			require.Equal(t, tc.wantConsumerSessions, got.ActiveConsumerSessionCount)

			gotDiagnostics := make([]*tunnelDiagnosticsResult, 0, len(got.Connections))
			for _, connection := range got.Connections {
				var diagnostics *tunnelDiagnosticsResult
				if d := connection.Diagnostics; d != nil {
					diagnostics = &tunnelDiagnosticsResult{State: d.State, TargetState: d.TargetState, ConsecutiveFailures: d.ConsecutiveFailures, DNS: d.DNS, TCP: d.TCP, TLS: d.TLS}
				}
				gotDiagnostics = append(gotDiagnostics, diagnostics)
				connection.Diagnostics = nil
			}
			require.Equal(t, tc.wantConnections, got.Connections)
			require.Equal(t, tc.wantDiagnostics, gotDiagnostics)
		})
	}
}
