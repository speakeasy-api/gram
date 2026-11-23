package tailnetgateway

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortPoolDisabledLetsTsnetPick(t *testing.T) {
	t.Parallel()
	pool := newPortPool(0, 3)
	for range 5 {
		port, err := pool.acquire()
		require.NoError(t, err)
		require.Zero(t, port)
	}
	pool.release(0)
}

func TestPortPoolHandsOutEachPortOnce(t *testing.T) {
	t.Parallel()
	pool := newPortPool(29000, 3)
	seen := map[uint16]bool{}
	for range 3 {
		port, err := pool.acquire()
		require.NoError(t, err)
		require.GreaterOrEqual(t, port, uint16(29000))
		require.Less(t, port, uint16(29003))
		require.False(t, seen[port], "port %d handed out twice", port)
		seen[port] = true
	}
	_, err := pool.acquire()
	require.ErrorIs(t, err, errNoTsnetPort)
}

func TestPortPoolReusesReleasedPortsLast(t *testing.T) {
	t.Parallel()
	pool := newPortPool(29000, 3)
	first, err := pool.acquire()
	require.NoError(t, err)
	require.Equal(t, uint16(29000), first)

	pool.release(first)
	next, err := pool.acquire()
	require.NoError(t, err)
	require.Equal(t, uint16(29001), next, "a just-released port waits until the others are taken")

	_, err = pool.acquire()
	require.NoError(t, err)
	again, err := pool.acquire()
	require.NoError(t, err)
	require.Equal(t, first, again)
}

func TestPortPoolIgnoresPortsOutsideItsRange(t *testing.T) {
	t.Parallel()
	pool := newPortPool(29000, 1)
	port, err := pool.acquire()
	require.NoError(t, err)
	pool.release(28999)
	pool.release(29001)
	_, err = pool.acquire()
	require.ErrorIs(t, err, errNoTsnetPort, "releasing foreign ports must not free the held one")
	pool.release(port)
	_, err = pool.acquire()
	require.NoError(t, err)
}

type closeCounter struct {
	Node
	closes int
	err    error
}

func (c *closeCounter) Close(context.Context) error {
	c.closes++
	return c.err
}

func TestPortNodeReleasesOnceAfterClose(t *testing.T) {
	t.Parallel()
	releases := 0
	inner := &closeCounter{Node: nil, closes: 0, err: errors.New("listener shutdown timed out")}
	node := &portNode{Node: inner, once: sync.Once{}, release: func() {
		require.Equal(t, 1, inner.closes, "the port is released only after the node closed")
		releases++
	}}

	require.Error(t, node.Close(t.Context()), "a close error is still returned")
	_ = node.Close(t.Context())
	require.Equal(t, 1, releases, "a failed close still releases, and only once")
}
