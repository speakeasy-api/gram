package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireLoopbackAcceptsLoopbackHosts(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{"127.0.0.1:4100", "127.0.0.2:4100", "[::1]:4100", "localhost:4100"} {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, requireLoopback(addr))
		})
	}
}

func TestRequireLoopbackRejectsOtherHosts(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{":4100", "0.0.0.0:4100", "[::]:4100", "192.168.1.10:4100", "example.test:4100", "127.0.0.1"} {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			require.Error(t, requireLoopback(addr))
		})
	}
}
