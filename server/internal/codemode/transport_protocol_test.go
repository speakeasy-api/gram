package codemode

import (
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

// A runner error keeps the output printed before the failure. A runner that
// sends no output field still has its error reported.
func TestRunnerErrorCarriesPrintedOutput(t *testing.T) {
	t.Parallel()
	never := func(runnerMessage) bool { return false }
	for _, tc := range []struct {
		name      string
		message   runnerMessage
		code      string
		output    string
		truncated bool
	}{
		{"without output", runnerMessage{Type: "error", Code: "execution_failed", Message: "ZeroDivisionError: division by zero"}, "execution_failed", "", false},
		{"with output", runnerMessage{Type: "error", Code: "execution_failed", Message: "ZeroDivisionError: division by zero", Output: "before traceback\n"}, "execution_failed", "before traceback\n", false},
		{"with truncated output", runnerMessage{Type: "error", Code: "deadline_exceeded", Message: "execution deadline exceeded", Output: strings.Repeat("x", maxPrintBytes), OutputTruncated: true}, "deadline_exceeded", strings.Repeat("x", maxPrintBytes), true},
		{"with oversized output", runnerMessage{Type: "error", Code: "execution_failed", Message: "failed", Output: strings.Repeat("x", maxPrintBytes+1)}, "protocol_error", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			script := func(string) []runnerMessage { return []runnerMessage{tc.message} }
			client, err := NewRunnerClient(startedRunner(t, true, script, never), strings.Repeat("t", 32), &net.Dialer{})
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			result := client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
			require.NotNil(t, result.Error)
			require.Equal(t, tc.code, result.Error.Code)
			require.Equal(t, tc.output, result.Output)
			require.Equal(t, tc.truncated, result.OutputTruncated)
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
