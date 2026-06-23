package gateway

import (
	"io"
	"os"
)

// yamuxLogOutput is where the yamux session logs go. Defaults to discard;
// set TUNNEL_YAMUX_DEBUG to surface yamux's internal session diagnostics.
var yamuxLogOutput io.Writer = func() io.Writer {
	if os.Getenv("TUNNEL_YAMUX_DEBUG") != "" {
		return os.Stderr
	}
	return io.Discard
}()
