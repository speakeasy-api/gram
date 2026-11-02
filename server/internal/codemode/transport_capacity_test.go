package codemode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"
)

// upgradingRunner accepts runner upgrades and answers with capacityHeader
// (omitted when empty), then serves yamux so the client session stays open.
func upgradingRunner(t *testing.T, capacityHeader string) string {
	t.Helper()
	return scriptedRunner(t, capacityHeader, nil, nil)
}

// scriptedRunner is upgradingRunner with hooks: upgraded runs once the
// upgrade response is written, and serve, when set, handles each stream the
// client opens.
func scriptedRunner(t *testing.T, capacityHeader string, upgraded func(), serve func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				reader := bufio.NewReader(conn)
				if _, err := http.ReadRequest(reader); err != nil {
					_ = conn.Close()
					return
				}
				response := "HTTP/1.1 101 Switching Protocols\r\nConnection: upgrade\r\nUpgrade: gram-code-yamux\r\nGram-Code-Protocol: 1\r\n"
				if capacityHeader != "" {
					response += fmt.Sprintf("Gram-Code-Capacity: %s\r\n", capacityHeader)
				}
				if _, err := conn.Write([]byte(response + "\r\n")); err != nil {
					_ = conn.Close()
					return
				}
				if upgraded != nil {
					upgraded()
				}
				session, err := yamux.Server(conn, nil)
				if err != nil {
					_ = conn.Close()
					return
				}
				t.Cleanup(func() { _ = session.Close() })
				for serve != nil {
					stream, err := session.Accept()
					if err != nil {
						return
					}
					go serve(stream)
				}
			}()
		}
	}()
	return "http://" + listener.Addr().String()
}

func TestRunnerClientLearnsReportedCapacity(t *testing.T) {
	t.Parallel()
	client, err := NewRunnerClient(upgradingRunner(t, "4"), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.Zero(t, client.knownCapacity(), "capacity is unknown before the runner is reached")
	_, err = client.currentSession(t.Context())
	require.NoError(t, err)
	require.Equal(t, 4, client.knownCapacity())
	require.True(t, client.streams.TryAcquire(4))
	defer client.streams.Release(4)
	require.False(t, client.streams.TryAcquire(1), "streams beyond the runner's capacity wait for a slot instead of being refused")
}

func TestRunnerClientNeverOpensMoreStreamsThanTheRunnerAdmits(t *testing.T) {
	t.Parallel()
	const capacity = 2
	var open, refused atomic.Int32
	endpoint := scriptedRunner(t, fmt.Sprint(capacity), nil, func(stream net.Conn) {
		defer func() { _ = stream.Close() }()
		frame, err := readRunnerFrame(stream)
		if err != nil {
			return
		}
		var start runnerStart
		if json.Unmarshal(frame, &start) != nil {
			return
		}
		// Deferred after the close above, so the count drops before the
		// client can see the stream end.
		defer open.Add(-1)
		if open.Add(1) > capacity {
			refused.Add(1)
			_ = writeRunnerFrame(stream, runnerMessage{Type: "error", Code: "capacity_exceeded", Message: "full"})
			return
		}
		if writeRunnerFrame(stream, runnerMessage{Type: "started", ExecutionID: start.ExecutionID}) != nil {
			return
		}
		<-time.After(20 * time.Millisecond)
		_ = writeRunnerFrame(stream, runnerMessage{Type: "complete", Value: json.RawMessage(`1`)})
		_, _ = readRunnerFrame(stream)
	})
	client, err := NewRunnerClient(endpoint, strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	// The burst starts before the client has learned the runner's capacity.
	var wg sync.WaitGroup
	for range RunnerStreams {
		wg.Go(func() {
			result := client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
			require.Nil(t, result.Error)
		})
	}
	wg.Wait()
	require.Zero(t, refused.Load(), "the client opened more streams than the runner admits")
}

func TestRunnerClientCapsReportedCapacity(t *testing.T) {
	t.Parallel()
	client, err := NewRunnerClient(upgradingRunner(t, "99"), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.currentSession(t.Context())
	require.NoError(t, err)
	require.Equal(t, RunnerStreams, client.knownCapacity(), "the client never opens more than its stream limit")
}

func TestRunnerClientRequiresReportedCapacity(t *testing.T) {
	t.Parallel()
	for _, header := range []string{"", "0", "twelve"} {
		client, err := NewRunnerClient(upgradingRunner(t, header), strings.Repeat("t", 32), &net.Dialer{})
		require.NoError(t, err)
		_, err = client.currentSession(t.Context())
		require.ErrorContains(t, err, "did not report its capacity", "header %q", header)
		_ = client.Close()
	}
}

func TestRunnerStreamWaitIsNotRunTime(t *testing.T) {
	t.Parallel()
	const hold = 200 * time.Millisecond
	endpoint := scriptedRunner(t, "1", nil, func(stream net.Conn) {
		defer func() { _ = stream.Close() }()
		frame, err := readRunnerFrame(stream)
		if err != nil {
			return
		}
		var start runnerStart
		if json.Unmarshal(frame, &start) != nil {
			return
		}
		if writeRunnerFrame(stream, runnerMessage{Type: "started", ExecutionID: start.ExecutionID}) != nil {
			return
		}
		<-time.After(hold)
		_ = writeRunnerFrame(stream, runnerMessage{Type: "complete", Value: json.RawMessage(`1`)})
		_, _ = readRunnerFrame(stream)
	})
	client, err := NewRunnerClient(endpoint, strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	results := make([]*ExecutionResult, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			results[i] = client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
		})
	}
	wg.Wait()
	for _, result := range results {
		require.Nil(t, result.Error)
		require.GreaterOrEqual(t, result.Timing.Run, hold)
		// One execution waited a whole hold for the single stream; that wait
		// is admission, not run time.
		require.Less(t, result.Timing.Run, hold+hold*3/4)
	}
}

// completingRunner answers every execution with the value 1.
func completingRunner(t *testing.T, capacityHeader string) string {
	t.Helper()
	return scriptedRunner(t, capacityHeader, nil, func(stream net.Conn) {
		defer func() { _ = stream.Close() }()
		frame, err := readRunnerFrame(stream)
		if err != nil {
			return
		}
		var start runnerStart
		if json.Unmarshal(frame, &start) != nil {
			return
		}
		if writeRunnerFrame(stream, runnerMessage{Type: "started", ExecutionID: start.ExecutionID}) != nil {
			return
		}
		_ = writeRunnerFrame(stream, runnerMessage{Type: "complete", Value: json.RawMessage(`1`)})
		_, _ = readRunnerFrame(stream)
	})
}

func TestRunnerStreamWaitReconnectsAClosedSession(t *testing.T) {
	t.Parallel()
	client, err := NewRunnerClient(completingRunner(t, "1"), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	first, err := client.currentSession(t.Context())
	require.NoError(t, err)
	require.True(t, client.streams.TryAcquire(1), "hold the runner's only stream")
	done := make(chan *ExecutionResult, 1)
	go func() {
		done <- client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
	}()
	require.Never(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 50*time.Millisecond, 10*time.Millisecond, "the execution waits while the stream is held")
	_ = first.Close()
	client.streams.Release(1)
	result := <-done
	require.Nil(t, result.Error, "%+v", result.Error)
}

func TestRunnerAdmissionWaitBoundsConnecting(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		// Accept and never answer the upgrade.
		var held []net.Conn
		for {
			conn, err := listener.Accept()
			if err != nil {
				for _, c := range held {
					_ = c.Close()
				}
				return
			}
			held = append(held, conn)
		}
	}()
	client, err := NewRunnerClient("http://"+listener.Addr().String(), strings.Repeat("t", 32), &net.Dialer{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	started := time.Now()
	result := client.Run(t.Context(), uuid.New(), "1", func(context.Context) (Host, error) { return idleHost{}, nil })
	require.NotNil(t, result.Error)
	require.Equal(t, "capacity_exceeded", result.Error.Code)
	require.Less(t, time.Since(started), connectTimeout, "the admission wait, not the connect timeout, bounds a hung handshake")
}
