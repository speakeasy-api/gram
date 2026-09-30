package mv

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
