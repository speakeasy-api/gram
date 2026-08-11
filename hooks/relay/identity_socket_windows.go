//go:build windows

package relay

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialAgentSocket(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
