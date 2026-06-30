package gateway

import (
	"io"
	"os"
)

// Set TUNNEL_YAMUX_DEBUG to surface yamux internals; default is discard.
var yamuxLogOutput io.Writer = func() io.Writer {
	if os.Getenv("TUNNEL_YAMUX_DEBUG") != "" {
		return os.Stderr
	}
	return io.Discard
}()
