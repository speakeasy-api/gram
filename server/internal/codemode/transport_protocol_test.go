package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// startedRunner reads the start frame, optionally acknowledges it, and then
// sends each frame produced by script, reading one reply after every frame
// for which reply is true.
func startedRunner(t *testing.T, acknowledge bool, script func(executionID string) []runnerMessage, reply func(runnerMessage) bool) string {
	t.Helper()
	return scriptedRunner(t, "4", nil, func(stream net.Conn) {
		defer func() { _ = stream.Close() }()
		frame, err := readRunnerFrame(stream)
		if err != nil {
			return
		}
		var start runnerStart
		if json.Unmarshal(frame, &start) != nil {
			return
		}
		if acknowledge && writeRunnerFrame(stream, runnerMessage{Type: "started", ExecutionID: start.ExecutionID}) != nil {
			return
		}
		for _, message := range script(start.ExecutionID) {
			if writeRunnerFrame(stream, message) != nil {
				return
			}
			if reply(message) {
				if _, err := readRunnerFrame(stream); err != nil {
					return
				}
			}
		}
		_, _ = readRunnerFrame(stream)
	})
}

func serversCallback(id uint32) runnerMessage {
	return runnerMessage{Type: "callback", ID: id, Method: "servers", Arguments: json.RawMessage(`{}`)}
}

func TestRunnerCallbackProtocolViolations(t *testing.T) {
	t.Parallel()
	never := func(runnerMessage) bool { return false }
	always := func(runnerMessage) bool { return true }
	for _, tc := range []struct {
		name        string
		acknowledge bool
		script      func(string) []runnerMessage
		reply       func(runnerMessage) bool
		code        string
	}{
		{"callback before started", false, func(string) []runnerMessage { return []runnerMessage{serversCallback(1)} }, never, "protocol_error"},
		{"duplicate callback", true, func(string) []runnerMessage { return []runnerMessage{serversCallback(1), serversCallback(1)} }, always, "protocol_error"},
		{"too many callbacks", true, func(string) []runnerMessage {
			messages := make([]runnerMessage, 0, maxCallbacks+1)
			for id := range uint32(maxCallbacks + 1) {
				messages = append(messages, serversCallback(id+1))
			}
			return messages
		}, always, "callback_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewRunnerClient(startedRunner(t, tc.acknowledge, tc.script, tc.reply), strings.Repeat("t", 32), &net.Dialer{})
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			result := client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
			require.NotNil(t, result.Error)
			require.Equal(t, tc.code, result.Error.Code)
		})
	}
}

// pendingHost holds every tool call until its execution ends.
type pendingHost struct{ idleHost }

func (pendingHost) Call(ctx context.Context, _ string, _ json.RawMessage) (*ToolResult, error) {
	<-ctx.Done()
	return &ToolResult{Outcome: "unknown", Error: "tool_call_outcome_unknown"}, nil
}

// TestConcurrencyLimitRecordsEveryReceivedCall stops a run at its ninth
// concurrent helper while eight calls are in flight, whether the runner or the
// server enforces the limit. Every call the server received must appear in
// ToolCalls, so a caller never replays one blindly.
func TestConcurrencyLimitRecordsEveryReceivedCall(t *testing.T) {
	t.Parallel()
	call := func(id uint32) runnerMessage {
		return runnerMessage{Type: "callback", ID: id, Method: "call", Arguments: json.RawMessage(`{"path":"remote--write","arguments":{}}`)}
	}
	for _, tc := range []struct {
		name  string
		last  runnerMessage
		code  string
		calls int
	}{
		{"runner refuses the ninth", runnerMessage{Type: "error", Code: "execution_failed", Message: "at most eight callbacks may run concurrently; the call over the limit was not dispatched"}, "execution_failed", maxConcurrentCallbacks},
		{"server refuses the ninth", call(maxConcurrentCallbacks + 1), "callback_limit", maxConcurrentCallbacks + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One write delivers every frame at once, as a burst from a remote
			// runner does, so the run can end before any call is dispatched.
			endpoint := scriptedRunner(t, "4", nil, func(stream net.Conn) {
				defer func() { _ = stream.Close() }()
				frame, err := readRunnerFrame(stream)
				if err != nil {
					return
				}
				var start runnerStart
				if json.Unmarshal(frame, &start) != nil {
					return
				}
				var burst bytes.Buffer
				_ = writeRunnerFrame(&burst, runnerMessage{Type: "started", ExecutionID: start.ExecutionID})
				for id := range uint32(maxConcurrentCallbacks) {
					_ = writeRunnerFrame(&burst, call(id+1))
				}
				_ = writeRunnerFrame(&burst, tc.last)
				if _, err := stream.Write(burst.Bytes()); err != nil {
					return
				}
				_, _ = readRunnerFrame(stream)
			})
			client, err := NewRunnerClient(endpoint, strings.Repeat("t", 32), &net.Dialer{})
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			result := client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return pendingHost{}, nil })
			require.NotNil(t, result.Error)
			require.Equal(t, tc.code, result.Error.Code)
			require.Len(t, result.ToolCalls, tc.calls)
			for _, outcome := range result.ToolCalls {
				require.Equal(t, "remote--write", outcome.Path)
				require.Contains(t, []string{"unknown", "not_dispatched"}, outcome.Outcome)
			}
			if tc.calls > maxConcurrentCallbacks {
				require.Equal(t, "not_dispatched", result.ToolCalls[maxConcurrentCallbacks].Outcome, "the refused call never reaches its backend")
			}
		})
	}
}

// TestCancellationAfterHandshakeKeepsSession cancels each connection attempt
// as the runner finishes its upgrade. Whatever connect returns, a session it
// hands back must not carry the interrupted handshake's deadline.
func TestCancellationAfterHandshakeKeepsSession(t *testing.T) {
	t.Parallel()
	cancels := make(chan context.CancelFunc, 1)
	endpoint := scriptedRunner(t, "4", func() { (<-cancels)() }, nil)
	client, err := NewRunnerClient(endpoint, strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	for range 200 {
		ctx, cancel := context.WithCancel(t.Context())
		cancels <- cancel
		session, _, err := client.connect(ctx)
		cancel()
		if err != nil {
			continue
		}
		_, err = session.Ping()
		require.NoError(t, err, "a cancellation after the handshake must not tear down the session")
		_ = session.Close()
	}
}
