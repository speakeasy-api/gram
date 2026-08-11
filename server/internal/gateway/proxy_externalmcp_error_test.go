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
// The rune here is three bytes wide so that the limit does not divide it
// evenly — a two-byte rune lands flush against a 512-byte cut and never
// exercises the split.
func TestExternalMCPErrorSummaryTruncatesToValidUTF8(t *testing.T) {
	t.Parallel()

	multibyte := []json.RawMessage{json.RawMessage(strings.Repeat("€", externalMCPErrorSummaryLimit))}
	summary := externalMCPErrorSummary(multibyte)

	require.Equal(t, summary, strings.ToValidUTF8(summary, "�"),
		"summary must not end in a split rune")
}

// Guards the returned bound and the dropping of parts past the budget. That
// the limit also bounds the *builder* — so a multi-megabyte part is sliced on
// the way in rather than copied whole and trimmed after — is not observable
// through the return value, and is not covered here.
func TestExternalMCPErrorSummaryDropsPartsPastTheBudget(t *testing.T) {
	t.Parallel()

	huge := []json.RawMessage{
		json.RawMessage(strings.Repeat("a", externalMCPErrorSummaryLimit*20)),
		json.RawMessage(strings.Repeat("b", externalMCPErrorSummaryLimit*20)),
	}
	summary := externalMCPErrorSummary(huge)

	require.LessOrEqual(t, len(summary), externalMCPErrorSummaryLimit+len("…(truncated)"))
	require.NotContains(t, summary, "b", "the second part must never be written once the budget is spent")
}

// Content landing exactly on the limit with parts still to go drops the tail,
// so it has to be marked. Reading an unmarked summary as complete is the whole
// failure mode this marker exists to prevent.
func TestExternalMCPErrorSummaryMarksTruncationAtExactLimit(t *testing.T) {
	t.Parallel()

	exact := []json.RawMessage{
		json.RawMessage(strings.Repeat("a", externalMCPErrorSummaryLimit)),
		json.RawMessage("Bad_OAuth_Token"),
	}
	summary := externalMCPErrorSummary(exact)

	require.Contains(t, summary, "(truncated)")
}

// The mirror case: content that exactly fills the budget with nothing left
// over is complete, and marking it would misreport an intact summary.
func TestExternalMCPErrorSummaryNoMarkerWhenContentExactlyFits(t *testing.T) {
	t.Parallel()

	exact := []json.RawMessage{json.RawMessage(strings.Repeat("a", externalMCPErrorSummaryLimit))}
	summary := externalMCPErrorSummary(exact)

	require.NotContains(t, summary, "(truncated)")
	require.Len(t, summary, externalMCPErrorSummaryLimit)
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
