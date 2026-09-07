package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

// recordedProtocolVersionAttrs runs recordMCPProtocolVersionSpan inside a
// span and returns the attributes it left behind.
func recordedProtocolVersionAttrs(t *testing.T, requested, negotiated string) map[string]string {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	ctx, span := provider.Tracer("test").Start(t.Context(), "mcp")
	recordMCPProtocolVersionSpan(ctx, requested, negotiated)
	span.End()

	ended := recorder.Ended()
	require.Len(t, ended, 1)

	got := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	return got
}

// TestRecordMCPProtocolVersionSpan_RecordsDowngradeAsADiscrepancy is the case
// that motivates tracking the two versions separately: a client requesting a
// revision outside the supported set is negotiated down to a different one,
// and collapsing the two into one attribute would hide that.
func TestRecordMCPProtocolVersionSpan_RecordsDowngradeAsADiscrepancy(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, mcpversions.Version20260728, mcpversions.Version20251125)
	require.Equal(t, mcpversions.Version20260728, got[string(attr.McpRequestedProtocolVersionKey)])
	require.Equal(t, mcpversions.Version20251125, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

func TestRecordMCPProtocolVersionSpan_OmitsAbsentRequestedVersion(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, "", mcpversions.Version20250326)
	require.NotContains(t, got, string(attr.McpRequestedProtocolVersionKey))
	require.Equal(t, mcpversions.Version20250326, got[string(attr.McpNegotiatedProtocolVersionKey)])
}

// TestRecordMCPProtocolVersionSpan_OmitsAbsentNegotiatedVersion covers the
// session-store propagation path, which knows what the client requested but
// not what it was answered with.
func TestRecordMCPProtocolVersionSpan_OmitsAbsentNegotiatedVersion(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, mcpversions.Version20250618, "")
	require.Equal(t, mcpversions.Version20250618, got[string(attr.McpRequestedProtocolVersionKey)])
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}

func TestRecordMCPProtocolVersionSpan_RecordsNothingWhenBothAbsent(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, "", "")
	require.NotContains(t, got, string(attr.McpRequestedProtocolVersionKey))
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}

// TestRecordMCPProtocolVersionSpan_KeepsUnrecognizedVersions pins that the span
// carries the raw value. Bucketing to "other" is a metric-dimension concern;
// on a span an unrecognized revision is the diagnostic payload.
func TestRecordMCPProtocolVersionSpan_KeepsUnrecognizedVersions(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, "1999-12-31", mcpversions.Version20250326)
	require.Equal(t, "1999-12-31", got[string(attr.McpRequestedProtocolVersionKey)])
}

func TestRecordMCPProtocolVersionSpan_BoundsHostileValues(t *testing.T) {
	t.Parallel()

	got := recordedProtocolVersionAttrs(t, strings.Repeat("a", 4096), "2025-06-18\x00injected")
	require.Len(t, got[string(attr.McpRequestedProtocolVersionKey)], 32)
	require.NotContains(t, got, string(attr.McpNegotiatedProtocolVersionKey))
}

func TestRecordMCPProtocolVersionSpan_ToleratesNonRecordingSpan(t *testing.T) {
	t.Parallel()

	// Sampled-out requests reach handlers with a non-recording span; recording
	// must be a no-op rather than a panic.
	require.NotPanics(t, func() {
		recordMCPProtocolVersionSpan(t.Context(), mcpversions.Version20250618, mcpversions.Version20250326)
	})
}
