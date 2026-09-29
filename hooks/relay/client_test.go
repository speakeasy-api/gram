package relay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// TestGateBudgetFollowsProviderDeadline: the budget follows the wall
// agenthooks already put on the handler context, not a per-provider constant.
func TestGateBudgetFollowsProviderDeadline(t *testing.T) {
	t.Parallel()

	t.Run("no deadline keeps the conservative default", func(t *testing.T) {
		t.Parallel()
		// The Pi/OpenClaw shim path: its gate deadline is enforced outside
		// this process, so nothing on the context describes it.
		require.Equal(t, gateSendBudget, gateBudget(context.Background()))
	})

	t.Run("a generous deadline is capped", func(t *testing.T) {
		t.Parallel()
		// Claude Code bakes --timeout=60s; agenthooks hands handlers 90% of it.
		ctx, cancel := context.WithTimeout(context.Background(), 54*time.Second)
		defer cancel()
		require.Equal(t, maxGateSendBudget, gateBudget(ctx))
	})

	t.Run("a deadline wider than the floor but under the cap is halved", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		budget := gateBudget(ctx)
		require.Greater(t, budget, gateSendBudget, "a 20s wall must buy more than the floor")
		require.LessOrEqual(t, budget, 10*time.Second)
	})

	t.Run("a tight deadline is never exceeded", func(t *testing.T) {
		t.Parallel()
		// The floor must not outrun the caller's own wall.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.LessOrEqual(t, gateBudget(ctx), 2*time.Second)
	})
}

// TestAttemptBoundsLeaveRoomForReplay is the regression that motivated
// DNO-1232: perAttemptTime (10s) exceeded gateSendBudget (5s), so one stalled
// connection consumed the whole budget and send never got a second attempt.
// The replay room now comes from bounding connect and handshake rather than
// from slicing the budget, which is what lets a slow control plane be waited
// on instead of cut off.
func TestAttemptBoundsLeaveRoomForReplay(t *testing.T) {
	t.Parallel()

	t.Run("a stalled connection leaves budget for replays", func(t *testing.T) {
		t.Parallel()
		require.Less(t, connectTimeout*2, gateSendBudget,
			"a stalled connect plus a replay's connect must fit inside the tightest budget")
	})

	t.Run("an attempt never outlives its budget", func(t *testing.T) {
		t.Parallel()
		for _, budget := range []time.Duration{gateSendBudget, maxGateSendBudget, sendBudget} {
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			timeout := attemptTimeout(ctx)
			cancel()
			require.LessOrEqual(t, timeout, perAttemptTime, "the transport ceiling still applies")
			require.LessOrEqual(t, timeout, budget, "an attempt must not outlive the budget it runs under")
		}
	})

	t.Run("no deadline keeps the transport ceiling", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, perAttemptTime, attemptTimeout(context.Background()))
	})

	t.Run("a spent budget still gets a real attempt", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		require.Equal(t, minAttemptTime, attemptTimeout(ctx),
			"a deadline that has all but passed must not produce an already-expired attempt")
	})

	t.Run("the transport bounds connect and handshake", func(t *testing.T) {
		t.Parallel()
		device, ok := ingestTransport().(*deviceTransport)
		require.True(t, ok)
		transport, ok := device.base.(*http.Transport)
		require.True(t, ok, "the bounded transport must survive the device wrapper")
		require.Equal(t, connectTimeout, transport.TLSHandshakeTimeout)
		require.NotNil(t, transport.DialContext)
	})
}

// TestClassifyTransportError: every transport failure reports statusCode 0, so
// the cause is the only thing telling them apart.
func TestClassifyTransportError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want failureCause
	}{
		{name: "nil is a definitive exchange", err: nil, want: causeNone},
		{
			name: "dns",
			err:  fmt.Errorf("post: %w", &net.DNSError{Err: "no such host", Name: "app.getgram.ai"}),
			want: causeDNS,
		},
		{
			name: "dns timeout reports as dns",
			err:  &net.DNSError{Err: "i/o timeout", Name: "app.getgram.ai", IsTimeout: true},
			want: causeDNS,
		},
		{
			name: "tls certificate rejection",
			err:  fmt.Errorf("handshake: %w", &tls.CertificateVerificationError{}),
			want: causeTLS,
		},
		{
			name: "tls unknown authority — the intercepting-proxy case",
			err:  fmt.Errorf("handshake: %w", x509.UnknownAuthorityError{}),
			want: causeTLS,
		},
		{
			name: "budget expiry",
			err:  fmt.Errorf("ingest: %w", context.DeadlineExceeded),
			want: causeTimeout,
		},
		{
			name: "caller gave up",
			err:  fmt.Errorf("ingest: %w", context.Canceled),
			want: causeCanceled,
		},
		{
			name: "connection refused",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			want: causeConnection,
		},
		{
			name: "connection reset",
			err:  &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET},
			want: causeConnection,
		},
		{name: "unrecognized", err: errors.New("something else"), want: causeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, classifyTransportError(tt.err))
		})
	}
}

// TestHTTPMessageNamesTheCause: a transport failure must say what failed
// instead of "HTTP 0".
func TestHTTPMessageNamesTheCause(t *testing.T) {
	t.Parallel()

	t.Run("a server message always wins", func(t *testing.T) {
		t.Parallel()
		res := ingestResult{
			statusCode: 0, decision: decision{Decision: "", Reason: "", Message: "Blocked by policy."},
			authRejected: false, failOpen: nil, skillCapture: nil, blockEffect: nil,
			cause: causeTimeout, causeDetail: "",
		}
		require.Equal(t, "Blocked by policy.", httpMessage(res))
	})

	t.Run("a real status is still reported as one", func(t *testing.T) {
		t.Parallel()
		res := ingestResult{
			statusCode: http.StatusBadGateway, decision: decision{Decision: "", Reason: "", Message: ""},
			authRejected: false, failOpen: nil, skillCapture: nil, blockEffect: nil,
			cause: causeNone, causeDetail: "",
		}
		require.Equal(t, "Speakeasy hook returned HTTP 502", httpMessage(res))
	})

	for _, cause := range []failureCause{causeDNS, causeTLS, causeTimeout, causeCanceled, causeConnection, causeUnknown} {
		t.Run("cause "+string(cause), func(t *testing.T) {
			t.Parallel()
			res := ingestResult{
				statusCode: 0, decision: decision{Decision: "", Reason: "", Message: ""},
				authRejected: false, failOpen: nil, skillCapture: nil, blockEffect: nil,
				cause: cause, causeDetail: "dial tcp: connection refused",
			}
			msg := httpMessage(res)
			require.NotContains(t, msg, "HTTP 0", "the opaque status must not reach the user")
			require.Contains(t, msg, "Speakeasy hooks")
			require.NotContains(t, msg, "dial tcp", "transport detail belongs in the debug log, not the tool call")
		})
	}
}

// TestInterpretErrorCarriesCause: the cause must survive the mapping onto
// ingestResult without disturbing the unsent classification that spools it.
func TestInterpretErrorCarriesCause(t *testing.T) {
	t.Parallel()

	res := interpretError(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
	require.Equal(t, 0, res.statusCode)
	require.Equal(t, causeConnection, res.cause)
	require.NotEmpty(t, res.causeDetail, "the debug log needs the underlying error text")
	require.True(t, res.unsent(), "a transport failure must still spool for replay")
}

// TestGateSurvivesSlowControlPlane is the end-to-end shape of the reported
// failure: a fail-closed org and a control plane that answers, but slower than
// the old flat 5s gate budget allowed. That budget expired, reported
// statusCode 0, and blocked the tool call; the derived one waits for the
// verdict.
func TestGateSurvivesSlowControlPlane(t *testing.T) {
	delay := gateSendBudget + 2*time.Second
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		time.Sleep(delay)
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	// Fail-closed: the posture whose users lose tool calls to a slow handshake.
	seedOrgSettings(t, cfg, false, time.Minute)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.NotContains(t, string(res.Stdout), `"permissionDecision":"deny"`,
		"a control plane that answers inside the budget must not block the call")
	require.NotContains(t, string(res.Stderr), "HTTP 0")
}
