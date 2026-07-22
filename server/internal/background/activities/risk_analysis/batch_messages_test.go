package risk_analysis

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
)

func TestParseRecordedToolCallsMalformedFallback(t *testing.T) {
	t.Parallel()

	calls := parseRecordedToolCalls(context.Background(), slog.New(slog.DiscardHandler), []byte(`rm -rf /tmp/x`)) //nolint:forbidigo // same-package test import-cycles with testenv

	require.Len(t, calls, 1)
	require.Equal(t, malformedToolCallsName, calls[0].Function.Name)
	require.Equal(t, `rm -rf /tmp/x`, calls[0].Function.Arguments)
}

func TestScanSurfaceIncludesToolRequestArgs(t *testing.T) {
	t.Parallel()

	req := toolReq("Bash")
	args := req.ToolCalls[0].Function.Arguments
	require.Equal(t, args, req.scanSurface())

	withContent := toolReq("Bash")
	withContent.Content = "running cleanup"
	require.Equal(t, "running cleanup\n"+args, withContent.scanSurface())

	multi := toolReq("Bash", "Write")
	require.Equal(t, args+"\n"+args, multi.scanSurface())

	emptyArgs := toolReq("Read")
	emptyArgs.ToolCalls[0].Function.Arguments = ""
	require.Empty(t, emptyArgs.scanSurface())

	user := msg(message.User)
	require.Equal(t, "content", user.scanSurface())
}

func TestMessageContentsUsesScanSurface(t *testing.T) {
	t.Parallel()

	contents := messageContents([]batchMessage{msg(message.ToolResponse), toolReq("Bash")})
	require.Equal(t, []string{"content", `{"command":"rm -rf /tmp/data"}`}, contents)
}
