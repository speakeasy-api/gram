package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type idleHost struct{}

func (idleHost) Servers(context.Context, PageArgs) (*ServerPage, error) {
	return nil, ErrToolUnavailable
}
func (idleHost) Search(context.Context, SearchArgs) (*SearchPage, error) {
	return nil, ErrToolUnavailable
}
func (idleHost) Describe(context.Context, string) (*Description, error) {
	return nil, ErrToolUnavailable
}
func (idleHost) Call(context.Context, string, json.RawMessage) (*ToolResult, error) {
	return nil, ErrToolUnavailable
}

// TestCancelledRunReleasesSlotDuringConnect holds the runner handshake open
// and cancels the waiting runs: each must return and free its stream slot
// right away instead of waiting for the connect timeout.
func TestCancelledRunReleasesSlotDuringConnect(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()

	client, err := NewRunnerClient("http://"+listener.Addr().String(), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	factory := func(context.Context) (Host, error) { return idleHost{}, nil }

	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan *ExecutionResult, RunnerStreams)
	for range RunnerStreams {
		go func() { results <- client.Run(ctx, uuid.New(), "1", factory) }()
	}
	require.Eventually(t, func() bool {
		if client.slots.TryAcquire(1) {
			client.slots.Release(1)
			return false
		}
		return true
	}, time.Second, 5*time.Millisecond, "every slot is held while the handshake hangs")

	cancelled := time.Now()
	cancel()
	for range RunnerStreams {
		select {
		case result := <-results:
			require.NotNil(t, result.Error)
			require.Equal(t, "cancelled", result.Error.Code)
		case <-time.After(time.Second):
			t.Fatal("a cancelled run kept waiting on the runner connection")
		}
	}
	require.Less(t, time.Since(cancelled), connectTimeout)
	require.True(t, client.slots.TryAcquire(RunnerStreams), "cancelled runs released their slots")
	client.slots.Release(RunnerStreams)
}

type callFailureHost struct {
	idleHost
	err error
}

func (h callFailureHost) Call(context.Context, string, json.RawMessage) (*ToolResult, error) {
	return nil, h.err
}

func TestCallbackMarksOnlyProvenRefusalsNotDispatched(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		err     error
		outcome string
	}{
		{"unclassified", fmt.Errorf("transport failed"), "unknown"},
		{"refused", &callNotDispatchedError{cause: ErrToolUnavailable}, "not_dispatched"},
		{"before-invoke", &DispatchError{BeforeInvoke: true, Code: "denied"}, "not_dispatched"},
		{"after-invoke", &DispatchError{BeforeInvoke: false, Code: "lost"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := &executionState{}
			_, err := state.callback(t.Context(), callFailureHost{err: tc.err}, runnerMessage{Method: "call", Arguments: json.RawMessage(`{"path":"server--tool","arguments":{}}`)})
			require.Error(t, err)
			require.Equal(t, []CallOutcome{{Path: "server--tool", Outcome: tc.outcome}}, state.calls)
		})
	}
}

func TestCallbackReplyCarriesExactlyOneResult(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value json.RawMessage
		err   error
		want  string
	}{
		{"value", json.RawMessage(`{"ok":true}`), nil, `{"type":"callback_result","id":1,"value":{"ok":true}}`},
		{"null value", json.RawMessage(`null`), nil, `{"type":"callback_result","id":1,"value":null}`},
		{"refusal", nil, ErrToolUnavailable, `{"type":"callback_result","id":1,"error":"tool operation unavailable or arguments invalid"}`},
		{"consent", nil, fmt.Errorf("call: %w", ErrConsentRequired), `{"type":"callback_result","id":1,"error":"this server changed since consent; the user must reconnect and give consent again"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(callbackReply(1, tc.value, tc.err))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(encoded))
		})
	}
}

// deadlineHost completes server--done and holds every other call until the
// execution ends.
type deadlineHost struct{ idleHost }

func (deadlineHost) Call(ctx context.Context, path string, _ json.RawMessage) (*ToolResult, error) {
	if path == "server--done" {
		return &ToolResult{Outcome: "completed", OK: true, Data: json.RawMessage(`{}`), Content: json.RawMessage(`[]`)}, nil
	}
	<-ctx.Done()
	return nil, fmt.Errorf("call %s: %w", path, ctx.Err())
}

const (
	// deadlineBatches runs of deadlineBatch executions each end at their
	// deadline. A stream timeout firing a moment before the context reported
	// about two runs in three as outcome_unknown, so 20 runs catch it.
	deadlineBatches = 5

	// deadlineBatch is the stream capacity startedRunner reports.
	deadlineBatch = 4
)

// TestDeadlineReportsDeadlineExceeded ends runs at their deadline with one
// call completed and another in flight. Whichever of the stream and the
// context notices the deadline first, the run reports deadline_exceeded and
// keeps both outcomes.
func TestDeadlineReportsDeadlineExceeded(t *testing.T) {
	t.Parallel()
	call := func(id uint32, path string) runnerMessage {
		return runnerMessage{Type: "callback", ID: id, Method: "call", Arguments: json.RawMessage(fmt.Sprintf(`{"path":%q,"arguments":{}}`, path))}
	}
	script := func(string) []runnerMessage { return []runnerMessage{call(1, "server--done"), call(2, "server--wait")} }
	answered := func(message runnerMessage) bool { return message.ID == 1 }
	client, err := NewRunnerClient(startedRunner(t, true, script, answered), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	// Connect first so no run spends its deadline on the handshake.
	_, err = client.currentSession(t.Context())
	require.NoError(t, err)
	factory := func(context.Context) (Host, error) { return deadlineHost{}, nil }
	codes := map[string]int{}
	for range deadlineBatches {
		results := make(chan *ExecutionResult, deadlineBatch)
		var runs sync.WaitGroup
		for range deadlineBatch {
			runs.Go(func() {
				// The program is submitted only with more than
				// runnerDeadlineLead left; this runner ignores its wall deadline.
				ctx, cancel := context.WithTimeout(t.Context(), runnerDeadlineLead+100*time.Millisecond)
				defer cancel()
				results <- client.Run(ctx, uuid.New(), "1", factory)
			})
		}
		runs.Wait()
		close(results)
		for result := range results {
			require.NotNil(t, result.Error)
			codes[result.Error.Code]++
			require.Equal(t, []CallOutcome{{Path: "server--done", Outcome: "completed"}, {Path: "server--wait", Outcome: "unknown"}}, result.ToolCalls)
		}
	}
	require.Equal(t, map[string]int{"deadline_exceeded": deadlineBatches * deadlineBatch}, codes)
}
