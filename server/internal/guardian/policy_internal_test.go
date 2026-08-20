package guardian

import (
	"context"
	"fmt"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDialResolvedAddressesRacesFallback(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	_, port, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	firstAttemptStarted := make(chan struct{})
	var closeStarted sync.Once
	dialer := &net.Dialer{
		Timeout:       time.Second,
		FallbackDelay: 10 * time.Millisecond,
		ControlContext: func(ctx context.Context, _, address string, _ syscall.RawConn) error {
			host, _, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return fmt.Errorf("split dial address: %w", splitErr)
			}
			if host != "192.0.2.1" {
				return nil
			}

			closeStarted.Do(func() { close(firstAttemptStarted) })
			<-ctx.Done()
			return ctx.Err()
		},
	}

	startedAt := time.Now()
	conn, err := dialResolvedAddresses(t.Context(), dialer, "tcp", port, []net.IP{
		net.ParseIP("192.0.2.1"),
		net.ParseIP("127.0.0.1"),
	})
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Less(t, time.Since(startedAt), 500*time.Millisecond)

	select {
	case <-firstAttemptStarted:
	default:
		require.Fail(t, "the stalled first address was not attempted")
	}
}

func TestInterleaveIPFamilies(t *testing.T) {
	t.Parallel()

	ips := interleaveIPFamilies([]net.IP{
		net.ParseIP("2001:4860:4860::8888"),
		net.ParseIP("2001:4860:4860::8844"),
		net.ParseIP("8.8.8.8"),
		net.ParseIP("8.8.4.4"),
	})

	require.Equal(t, []string{
		"2001:4860:4860::8888",
		"8.8.8.8",
		"2001:4860:4860::8844",
		"8.8.4.4",
	}, []string{ips[0].String(), ips[1].String(), ips[2].String(), ips[3].String()})
}
