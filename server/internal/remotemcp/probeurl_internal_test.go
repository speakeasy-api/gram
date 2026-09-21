package remotemcp

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// oversizedBody is one byte past what the probe is willing to read, so the
// size cap is the only thing that can reject these otherwise valid payloads.
func oversizedBody(prefix string, suffix string) []byte {
	padding := probeURLMaxBodyBytes + 1 - len(prefix) - len(suffix)
	return append(append([]byte(prefix), bytes.Repeat([]byte("a"), padding)...), suffix...)
}

func TestClassifyMCPSuccess_OversizedJSONIsNotMCP(t *testing.T) {
	t.Parallel()

	body := oversizedBody(`{"jsonrpc":"2.0","id":1,"result":{"pad":"`, `"}}`)
	require.Greater(t, len(body), probeURLMaxBodyBytes)

	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(bytes.NewReader(body)),
	}

	validMCP, err := classifyMCPSuccess(resp)
	require.NoError(t, err, "an oversized body is a classification, not a transport failure")
	require.False(t, validMCP)
}

func TestContainsJSONRPCSSEEvent_OversizedEventIsNotMCP(t *testing.T) {
	t.Parallel()

	// The event terminator sits past the cap, so the probe never sees the
	// dispatch even though the payload itself is a valid initialize response.
	event := append(oversizedBody(`data: {"jsonrpc":"2.0","id":1,"result":{"pad":"`, `"}}`), "\n\n"...)
	require.Greater(t, len(event), probeURLMaxBodyBytes)

	found, err := containsJSONRPCSSEEvent(bytes.NewReader(event))
	require.NoError(t, err, "an oversized event is a classification, not a transport failure")
	require.False(t, found)
}

func TestContainsJSONRPCSSEEvent_OversizedSingleLineIsNotMCP(t *testing.T) {
	t.Parallel()

	found, err := containsJSONRPCSSEEvent(strings.NewReader("data: " + strings.Repeat("a", probeURLMaxBodyBytes+1)))
	require.NoError(t, err, "an unterminated oversized line is a classification, not a transport failure")
	require.False(t, found)
}
