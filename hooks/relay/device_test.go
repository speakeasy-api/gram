package relay

import (
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

var traceparentRe = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`)

func TestIngestCarriesDeviceTelemetryHeaders(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	// The transport replays dropped connections under one idempotency key, so
	// a transient blip can legitimately deliver the event more than once; every
	// attempt carries the same device headers.
	require.GreaterOrEqual(t, fs.count(), 1)
	h := fs.headers[0]

	require.Regexp(t, traceparentRe, h.Get("traceparent"))
	require.Equal(t, runtime.GOOS, h.Get("X-Gram-Device-Os"))
	require.Equal(t, runtime.GOARCH, h.Get("X-Gram-Device-Arch"))
	require.Equal(t, BinaryVersion, h.Get("X-Gram-Device-Binary-Version"))
	require.Equal(t, "claude", h.Get("X-Gram-Device-Harness"))
	require.Equal(t, "cli", h.Get("X-Gram-Device-Harness-Variant"))
	require.Empty(t, h.Get("X-Gram-Device-Harness-Version"), "claude exposes no version to hook processes")

	elapsed, err := strconv.ParseInt(h.Get("X-Gram-Device-Elapsed-Ms"), 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, int64(0))
}

func TestSanitizeHeaderValueRejectsUnsafeEnvValues(t *testing.T) {
	require.Equal(t, "1.2.3", sanitizeHeaderValue(" 1.2.3 "))
	require.Empty(t, sanitizeHeaderValue("1.2\n3"), "control characters would make net/http reject the request at send time")
	require.Empty(t, sanitizeHeaderValue("v–1"), "non-ASCII drops the value")
	require.Len(t, sanitizeHeaderValue(strings.Repeat("9", 100)), 64)
}

func TestDeviceTraceParentStableWithinProcess(t *testing.T) {
	first := deviceTraceParent()
	require.Regexp(t, traceparentRe, first)
	require.Equal(t, first, deviceTraceParent(), "one hook invocation must stay one trace")
}
