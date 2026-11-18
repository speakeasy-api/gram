package proxy

import (
	"fmt"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/httpstream"
)

// writeSSERetry ends a stream the proxy closed on purpose, for shutdown or at
// its maximum age, with an SSE retry field: the polling signal MCP 2025-11-25
// asks for when a server closes a connection the client should reopen.
// Clients on earlier revisions reconnect regardless; the field only sets
// their delay. No event id is written, because ids must stay unique within
// the client's session and only the upstream assigns them.
func writeSSERetry(w http.ResponseWriter) (int64, error) {
	return writeSSEBytes(w, httpstream.RetryField(httpstream.RetryDelay()))
}

// writeSSEBytes writes one complete SSE frame and flushes it to the client.
func writeSSEBytes(w http.ResponseWriter, frame []byte) (int64, error) {
	n, err := w.Write(frame)
	if err != nil {
		return int64(n), fmt.Errorf("write sse frame: %w", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return int64(n), nil
}
