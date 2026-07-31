package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// toolCallAttributes is the slice of a telemetry row's attributes that carries
// the caller's identity. ClickHouse unflattens the dotted attribute keys the
// server writes (gram.mcp.client.name) into nested objects, so the shape here
// mirrors that nesting rather than the flat keys.
type toolCallAttributes struct {
	Gram struct {
		Mcp struct {
			Client struct {
				Name         string   `json:"name"`
				Version      string   `json:"version"`
				Capabilities []string `json:"capabilities"`
			} `json:"client"`
		} `json:"mcp"`
	} `json:"gram"`
}

// TestServePublic_ToolCallRecordsClientIdentity is the end-to-end proof of what
// the recording is for: a client hands over its identity at initialize, calls a
// tool some requests later, and the row written for that call in ClickHouse
// says who made it. Nothing between the handshake and the row carries the
// identity implicitly, so only a full round trip pins it.
func TestServePublic_ToolCallRecordsClientIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "client-info-telemetry")
	urns := addHTTPTools(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "tool_alpha")

	initializeBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"roots":    map[string]any{"listChanged": true},
				"sampling": map[string]any{},
			},
			"clientInfo": map[string]any{"name": "telemetry-client", "version": "4.2.0"},
		},
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, initializeBody, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize response: %s", w.Body.String())

	sessionID := w.Header().Get("Mcp-Session-Id")
	require.NotEmpty(t, sessionID)

	callStart := time.Now().UTC()

	// The tool resolves no server URL in this environment, so the call itself
	// fails. That is deliberate: a call is attributed to its caller whether or
	// not it succeeds, and asserting on the failing path proves the identity is
	// recorded before the outcome is known.
	_, _ = servePublicHTTP(t, ctx, ti, toolset.McpSlug.String, makeToolsCallBody("tool_alpha"), "", map[string]string{
		"Mcp-Session-Id": sessionID,
	})

	log := findToolCallLog(t, ctx, ti, authCtx.ProjectID.String(), urns["tool_alpha"].String(), callStart)

	var attrs toolCallAttributes
	require.NoError(t, json.Unmarshal([]byte(log.Attributes), &attrs))
	require.Equal(t, "telemetry-client", attrs.Gram.Mcp.Client.Name)
	require.Equal(t, "4.2.0", attrs.Gram.Mcp.Client.Version)
	require.Equal(t, []string{"roots", "sampling"}, attrs.Gram.Mcp.Client.Capabilities)
}

// findToolCallLog waits for the row written for a tool call to become
// queryable.
func findToolCallLog(t *testing.T, ctx context.Context, ti *testInstance, projectID, toolURN string, since time.Time) repo.TelemetryLog {
	t.Helper()

	var logs []repo.TelemetryLog
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

		var err error
		logs, err = repo.New(ti.chConn).ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
			GramProjectID: projectID,
			TimeStart:     since.Add(-1 * time.Minute).UnixNano(),
			TimeEnd:       since.Add(1 * time.Minute).UnixNano(),
			GramURNs:      []string{toolURN},
			SortOrder:     "desc",
			Limit:         10,
		})
		assert.NoError(c, err)
		assert.Len(c, logs, 1, "expected the tool call to write exactly one telemetry log")
	}, 10*time.Second, 50*time.Millisecond)

	return logs[0]
}
