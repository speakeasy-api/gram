package codemode

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"
)

// upgradingRunner accepts runner upgrades and answers with capacityHeader
// (omitted when empty), then serves yamux so the client session stays open.
func upgradingRunner(t *testing.T, capacityHeader string) string {
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
				session, err := yamux.Server(conn, nil)
				if err != nil {
					_ = conn.Close()
					return
				}
				t.Cleanup(func() { _ = session.Close() })
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
	require.Eventually(t, func() bool {
		if !client.slots.TryAcquire(4) {
			return false
		}
		defer client.slots.Release(4)
		if client.slots.TryAcquire(1) {
			client.slots.Release(1)
			return false
		}
		return true
	}, time.Second, 10*time.Millisecond, "executions beyond the runner's capacity wait for a slot instead of being refused")
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
