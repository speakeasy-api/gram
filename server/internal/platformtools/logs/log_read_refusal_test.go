package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// forbiddenTelemetry refuses the reads that need log:read, as the telemetry
// service does for a caller without it.
type forbiddenTelemetry struct {
	TelemetryService
}

func (forbiddenTelemetry) SearchLogs(context.Context, *telemetry.SearchLogsPayload) (*telemetry.SearchLogsResult, error) {
	return nil, oops.C(oops.CodeForbidden)
}

func (forbiddenTelemetry) SearchToolCalls(context.Context, *telemetry.SearchToolCallsPayload) (*telemetry.SearchToolCallsResult, error) {
	return nil, oops.C(oops.CodeForbidden)
}

func TestLogReadToolsExplainMissingLogRead(t *testing.T) {
	t.Parallel()

	for name, tool := range map[string]interface {
		Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error
	}{
		"search_logs":       NewSearchLogsTool(forbiddenTelemetry{}),
		"search_tool_calls": NewSearchToolCallsTool(forbiddenTelemetry{}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, tool.Call(t.Context(), toolconfig.ToolCallEnv{}, strings.NewReader("{}"), &out))

			var body map[string]string
			require.NoError(t, json.Unmarshal(out.Bytes(), &body))
			require.Equal(t, "log_read_required", body["error"])
			require.Contains(t, body["message"], "log:read")
		})
	}
}
