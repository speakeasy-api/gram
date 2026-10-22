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
	results := make(chan *ExecutionResult, maxRunnerStreams)
	for range maxRunnerStreams {
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
	for range maxRunnerStreams {
		select {
		case result := <-results:
			require.NotNil(t, result.Error)
			require.Equal(t, "admission_refused", result.Error.Code)
		case <-time.After(time.Second):
			t.Fatal("a cancelled run kept waiting on the runner connection")
		}
	}
	require.Less(t, time.Since(cancelled), connectTimeout)
	require.True(t, client.slots.TryAcquire(maxRunnerStreams), "cancelled runs released their slots")
	client.slots.Release(maxRunnerStreams)
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
