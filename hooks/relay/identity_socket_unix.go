//go:build !windows

package relay

import (
	"context"
	"net"
)

func dialAgentSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
