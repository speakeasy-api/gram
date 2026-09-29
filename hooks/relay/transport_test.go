package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"
)

// stallDials replaces the relay's dialer with one that imitates a network
// swallowing SYNs: every connection hangs until connectTimeout elapses and
// then reports the timeout the real net.Dialer would. Connection refusal
// (what an in-process listener can produce) is the easy case — it fails
// instantly and leaves the budget intact. The reported DNO-1230 failures are
// this one, where the stall itself is what consumes the gate.
func stallDials(t *testing.T) *atomic.Int64 {
	t.Helper()
	dials := &atomic.Int64{}
	prev := dialContext
	dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		select {
		case <-time.After(connectTimeout):
			return nil, &net.OpError{
				Op:     "dial",
				Net:    network,
				Source: nil,
				Addr:   nil,
				Err:    os.ErrDeadlineExceeded,
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() { dialContext = prev })
	return dials
}

// TestStalledConnectsAreRetriedWithinTheGateBudget is the DNO-1230
// regression. http.DefaultTransport's 30s dial timeout is six times
// gateSendBudget, so a stalled connection consumed the whole gate and the
// event was decided on a single attempt — one dropped packet, one blocked
// edit. Bounding connection setup buys the gate at least one fresh
// connection, which is what recovers from transient loss.
func TestStalledConnectsAreRetriedWithinTheGateBudget(t *testing.T) {
	setSpoolStateHome(t)
	dials := stallDials(t)
	cfg := authedConfig(t, "http://127.0.0.1:1")
	writeOrgSettings(cfg, false)

	start := time.Now()
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, dials.Load(), int64(2),
		"a stalled connect must leave budget for a retry on a fresh connection")
	require.Less(t, elapsed, 8*time.Second, "the gate must still resolve within its budget")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`,
		"a fail-closed org still blocks once the retries are exhausted")
}

// TestUnreachableControlPlaneBlocksWithActionableText: when the retries do
// run out, the developer reading the block needs to know the hook could not
// reach Gram and that retrying usually clears it. "Speakeasy hook returned
// HTTP 0" said neither.
func TestUnreachableControlPlaneBlocksWithActionableText(t *testing.T) {
	setSpoolStateHome(t)
	cfg := authedConfig(t, closedPortURL(t))
	writeOrgSettings(cfg, false)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	out := string(res.Stdout)
	require.Contains(t, out, `"permissionDecision":"deny"`)
	require.Contains(t, out, "could not reach the Gram control plane")
	require.Contains(t, out, "retry in a moment")
	require.NotContains(t, out, "HTTP 0", "the opaque status must not reach the user")
}

// TestUnreachableObserveDoesNotHoldTheAgent is the other half of DNO-1230.
// PostToolUse is fire and forget, but the agent still waits for the hook
// process, and an unreachable control plane used to hold it for the full 45s
// sendBudget: the SDK's connection retries and the relay's replays stacked,
// so the attempt cap bounded neither. The payload is spooled for the drain
// regardless, so the attempts are all the agent should ever wait for.
func TestUnreachableObserveDoesNotHoldTheAgent(t *testing.T) {
	setSpoolStateHome(t)
	dials := stallDials(t)
	cfg := authedConfig(t, "http://127.0.0.1:1")

	start := time.Now()
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	elapsed := time.Since(start)

	require.Equal(t, 0, res.ExitCode)
	require.LessOrEqual(t, dials.Load(), int64(maxSendAttempts),
		"the attempt cap must bound transport replays, not be multiplied by the SDK's own")
	require.Less(t, elapsed, maxSendAttempts*connectTimeout+2*time.Second,
		"an observed event must cost the attempts, not the whole send budget")
	require.Less(t, elapsed, sendBudget/2)
}

// TestStalledConnectsStillHonorFailOpen: the retries are a latency budget
// change, not a policy one — a fail-open org is still let through once the
// attempts are spent.
func TestStalledConnectsStillHonorFailOpen(t *testing.T) {
	setSpoolStateHome(t)
	stallDials(t)
	cfg := authedConfig(t, "http://127.0.0.1:1")
	writeOrgSettings(cfg, true)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.NotContains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// TestConnectTimeoutLeavesRoomForAReplay pins the relationship the fix rests
// on. gateSendBudget, connectTimeout and the replay pauses are tuned
// independently, so without this a later budget change could silently put
// the gate back on a single attempt.
func TestConnectTimeoutLeavesRoomForAReplay(t *testing.T) {
	twoAttempts := 2*connectTimeout + replayPause(0)
	require.Less(t, twoAttempts, gateSendBudget,
		"a gating event must afford a second connection after the first stalls")
	require.GreaterOrEqual(t, maxSendAttempts, 2)
}

func TestTransportFailureMessage(t *testing.T) {
	t.Run("names the dial failure without the request URL", func(t *testing.T) {
		err := fmt.Errorf("error sending request: %w", &url.Error{
			Op:  "Post",
			URL: "https://app.getgram.ai/rpc/hooks.ingest",
			Err: &net.OpError{Op: "dial", Net: "tcp", Source: nil, Addr: nil, Err: os.ErrDeadlineExceeded},
		})

		msg := transportFailureMessage(err)

		require.Contains(t, msg, "could not reach the Gram control plane")
		require.Contains(t, msg, "dial tcp: i/o timeout")
		require.NotContains(t, msg, "app.getgram.ai")
	})

	t.Run("reports an exhausted budget as a timeout", func(t *testing.T) {
		require.Contains(t, transportFailureMessage(context.DeadlineExceeded), "(timed out)")
	})

	t.Run("still advises a retry with no cause to report", func(t *testing.T) {
		msg := transportFailureMessage(nil)

		require.Contains(t, msg, "could not reach the Gram control plane")
		require.Contains(t, msg, "retry in a moment")
		require.NotContains(t, msg, "()")
	})

	t.Run("bounds a runaway cause to one line", func(t *testing.T) {
		msg := transportFailureMessage(errors.New(strings.Repeat("x", 400) + "\nsecond line"))

		require.Less(t, len(msg), 300)
		require.NotContains(t, msg, "\n")
		require.Contains(t, msg, "...")
	})
}

// TestHTTPMessageKeepsAnsweredStatuses: only an exchange that never reached
// the server gets the transport text. A status the server actually returned
// stays in the message, because it is the one thing an operator can look up.
func TestHTTPMessageKeepsAnsweredStatuses(t *testing.T) {
	msg := httpMessage(ingestResult{
		statusCode:   502,
		decision:     decision{Decision: "", Reason: "", Message: ""},
		authRejected: false,
		failOpen:     nil,
		skillCapture: nil,
		blockEffect:  nil,
	})

	require.Equal(t, "Speakeasy hook returned HTTP 502", msg)
}
