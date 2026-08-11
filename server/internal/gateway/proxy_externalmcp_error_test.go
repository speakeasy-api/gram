package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The provider's error code leads the payload, so it must survive into the log
// verbatim — that string is the whole reason this path is recorded.
func TestExternalMCPErrorSummaryKeepsProviderErrorCode(t *testing.T) {
	t.Parallel()

	content := []json.RawMessage{
		json.RawMessage(`{"type":"text","text":"INVALID_AUTH_HEADER: session expired"}`),
	}

	require.Contains(t, externalMCPErrorSummary(content), "INVALID_AUTH_HEADER")
}

// An errored result with no content still has to say something, or the log line
// reads as though nothing went wrong.
func TestExternalMCPErrorSummaryEmptyContent(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, externalMCPErrorSummary(nil))
	require.NotEmpty(t, externalMCPErrorSummary([]json.RawMessage{}))
}

// Content is upstream-controlled and can be a full response body, so the log
// must not carry it unbounded.
func TestExternalMCPErrorSummaryTruncatesLargeContent(t *testing.T) {
	t.Parallel()

	oversized := []json.RawMessage{json.RawMessage(strings.Repeat("a", externalMCPErrorSummaryLimit*3))}
	summary := externalMCPErrorSummary(oversized)

	require.Contains(t, summary, "(truncated)")
	require.Less(t, len(summary), externalMCPErrorSummaryLimit*2)
}

// Truncation cuts at a byte offset, so a multi-byte rune can straddle the
// boundary. The result still has to be valid UTF-8 to survive log encoding.
func TestExternalMCPErrorSummaryTruncatesToValidUTF8(t *testing.T) {
	t.Parallel()

	multibyte := []json.RawMessage{json.RawMessage(strings.Repeat("é", externalMCPErrorSummaryLimit))}
	summary := externalMCPErrorSummary(multibyte)

	require.Equal(t, summary, strings.ToValidUTF8(summary, "�"),
		"summary must not end in a split rune")
}

// Several content parts make up one failure; stopping at the first would drop
// the part naming the cause.
func TestExternalMCPErrorSummaryJoinsMultipleParts(t *testing.T) {
	t.Parallel()

	content := []json.RawMessage{
		json.RawMessage(`{"type":"text","text":"request failed"}`),
		json.RawMessage(`{"type":"text","text":"Bad_OAuth_Token"}`),
	}
	summary := externalMCPErrorSummary(content)

	require.Contains(t, summary, "request failed")
	require.Contains(t, summary, "Bad_OAuth_Token")
}
