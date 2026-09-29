package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type proxiedToolEventParams struct {
	projectID   string
	timestamp   time.Time
	mcpServerID string
	toolName    string
	userID      string
	statusCode  int
}

// insertProxiedToolEvent inserts the row the remote MCP proxy records for one
// tools/call it forwarded: an event_source of tool_call, the configured
// server's id, a tools: URN, a status code, a duration, and a trace id. It
// carries no toolset slug, which is what distinguishes it from a hosted call.
func insertProxiedToolEvent(t *testing.T, ctx context.Context, ti *testInstance, p proxiedToolEventParams) {
	t.Helper()

	attrs := map[string]any{
		"gram.event.source":            "tool_call",
		"gram.tool.name":               p.toolName,
		"gram.mcp_server.id":           p.mcpServerID,
		"http.response.status_code":    p.statusCode,
		"http.server.request.duration": 0.4,
		"user.id":                      p.userID,
	}
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)

	traceID := strings.ReplaceAll(uuid.New().String(), "-", "")
	spanID := traceID[:16]
	err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
		ID:                   uuid.New().String(),
		TimeUnixNano:         p.timestamp.UnixNano(),
		ObservedTimeUnixNano: p.timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "proxied tool event",
		TraceID:              &traceID,
		SpanID:               &spanID,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        p.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              "tools:remote:" + p.mcpServerID + ":" + p.toolName,
		ServiceName:          "gram-remote-mcp",
		ServiceVersion:       nil,
		GramChatID:           nil,
	}})
	require.NoError(t, err)
}

// TestGetMCPOutcomeBreakdown_MatchesProxiedCallsByConfiguredServerID pins that
// a remote server, which carries no toolset slug, is counted through the id the
// proxy stamps on its rows, and that another server's rows are not folded in.
func TestGetMCPOutcomeBreakdown_MatchesProxiedCallsByConfiguredServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	selected := uuid.New().String()
	other := uuid.New().String()

	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), mcpServerID: selected, toolName: "search", userID: "user-1", statusCode: 200})
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), mcpServerID: selected, toolName: "search", userID: "user-2", statusCode: 502})
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-3 * time.Minute), mcpServerID: other, toolName: "quote", userID: "user-3", statusCode: 200})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		MCPServerIDs:   []string{selected},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess:     1,
		telemetryRepo.MCPOutcomeServerError: 1,
	}, outcomeCounts(rows))
	require.Equal(t, map[string]uint64{telemetryRepo.MCPClientUnattributed: 2}, clientCounts(rows))

	// The organization-wide comparison, which names no server, now includes
	// proxied calls alongside hosted ones.
	all, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess:     2,
		telemetryRepo.MCPOutcomeServerError: 1,
	}, outcomeCounts(all))
}

// TestGetMCPOutcomeBreakdown_MatchesHookCallsByReportedServerName pins that a
// hook row with no resolved URL is still attributed when the agent's name for
// the server is one the configured server is known by, whatever its casing,
// and that URL-matched rows for the same server count alongside them.
func TestGetMCPOutcomeBreakdown_MatchesHookCallsByReportedServerName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	// Plugin-routed: Claude Code's derived prefix, no URL.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-6 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "plugin_acme-tools_External_Acme_Chat", toolName: "post_message", result: `"ok"`,
	})
	// Inventory-resolved display name as a client that normalizes names
	// reports it: sanitized and lower-cased, no URL.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-5 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "external_acme_chat", toolName: "post_message", errorMsg: "boom",
	})
	// URL-resolved slug.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-4 * time.Minute), traceID: uuid.New().String(),
		hookSource: "cursor", toolSource: "acme-chat", toolName: "post_message", result: `"ok"`,
		mcpServerURL: "https://api.example.test/mcp/acme-chat",
	})
	// A different server the same agent used must not be folded in.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-3 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "plugin_acme-tools_Shipping", toolName: "quote", result: `"ok"`,
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// ToolSources is the resolver's spelling set for the server: the configured
	// casing "External Acme Chat" differs from the inserted row, which matches
	// through its normalized variant "external_acme_chat" instead.
	rows, err := ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs:       []string{projectID},
		MCPServerURLSuffixes: []string{"/mcp/acme-chat"},
		ToolSources:          []string{"plugin_acme-tools_External_Acme_Chat", "External Acme Chat", "external_acme_chat"},
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{
		telemetryRepo.MCPOutcomeSuccess: 2,
		telemetryRepo.MCPOutcomeFailed:  1,
	}, outcomeCounts(rows))
	require.Equal(t, map[string]uint64{"claude-code": 2, "cursor": 1}, clientCounts(rows))

	// Reported names alone, without a URL identity, still select the server.
	rows, err = ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolSources:    []string{"plugin_acme-tools_External_Acme_Chat"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{telemetryRepo.MCPOutcomeSuccess: 1}, outcomeCounts(rows))

	// The column is compared by exact value, so a spelling outside the set
	// matches nothing: recall comes from the resolver listing every variant.
	rows, err = ti.chClient.GetMCPOutcomeBreakdown(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		ToolSources:    []string{"external acme chat"},
		TimeStart:      now.Add(-time.Hour).UnixNano(),
		TimeEnd:        now.UnixNano(),
	})
	require.NoError(t, err)
	require.Empty(t, outcomeCounts(rows))
}

// TestListMCPUsageUsers_ScopesByReportedServerName pins that the per-user
// reader accepts a reported name as a server identity rather than refusing
// the read as unscoped.
func TestListMCPUsageUsers_ScopesByReportedServerName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()

	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-5 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "plugin_acme-tools_external_acme_chat", toolName: "post_message", result: `"ok"`,
		userEmail: "alice@example.com",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-4 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "plugin_acme-tools_Shipping", toolName: "quote", result: `"ok"`,
		userEmail: "bob@example.com",
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	users, err := ti.chClient.ListMCPUsageUsers(ctx, telemetryRepo.GetMCPOutcomeBreakdownParams{
		GramProjectIDs: []string{projectID},
		// The configured casing differs from alice's row; the lower-cased
		// variant beside it is what matches.
		ToolSources: []string{"plugin_acme-tools_External_Acme_Chat", "plugin_acme-tools_external_acme_chat"},
		TimeStart:   now.Add(-time.Hour).UnixNano(),
		TimeEnd:     now.UnixNano(),
		Limit:       10,
	})
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, "email", users[0].IdentityKind)
	require.Equal(t, "alice@example.com", users[0].Identifier)
}

// TestGetActiveCounts_ScopesByConfiguredServerID pins that active users can be
// counted for a proxied server through the id on its rows.
func TestGetActiveCounts_ScopesByConfiguredServerID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()
	selected := uuid.New().String()
	other := uuid.New().String()

	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-5 * time.Minute), mcpServerID: selected, toolName: "search", userID: "user-1", statusCode: 200})
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-4 * time.Minute), mcpServerID: selected, toolName: "search", userID: "user-1", statusCode: 200})
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-3 * time.Minute), mcpServerID: selected, toolName: "search", userID: "user-2", statusCode: 200})
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-2 * time.Minute), mcpServerID: other, toolName: "quote", userID: "user-3", statusCode: 200})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	counts, err := ti.chClient.GetActiveCounts(ctx, telemetryRepo.GetActiveCountsParams{
		GramProjectID: projectID,
		TimeStart:     now.Add(-time.Hour).UnixNano(),
		TimeEnd:       now.UnixNano(),
		MCPServerID:   selected,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(2), counts.ActiveUsersCount)
}

// TestGetActiveCounts_CountsHookObservedUsers pins that a server's active users
// include the people whose calls only an agent hook observed. Hook rows carry
// no mcp_server_id and a hooks: URN, so a read scoped by the configured id
// alone cannot see them; the hook identities the outcome tally matches on must
// select them here too, or active_users is zero beside a nonzero tally.
func TestGetActiveCounts_CountsHookObservedUsers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()
	now := time.Now().UTC()
	selected := uuid.New().String()

	// One user through the gateway.
	insertProxiedToolEvent(t, ctx, ti, proxiedToolEventParams{projectID: projectID, timestamp: now.Add(-6 * time.Minute), mcpServerID: selected, toolName: "post_message", userID: "user-1", statusCode: 200})
	// Two more only a hook saw: one by the plugin-routed name, one by the URL.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-5 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "external_acme_chat", toolName: "post_message", result: `"ok"`,
		userEmail: "alice@example.com",
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-4 * time.Minute), traceID: uuid.New().String(),
		hookSource: "cursor", toolSource: "acme-chat", toolName: "post_message", result: `"ok"`,
		mcpServerURL: "https://api.example.test/mcp/acme-chat", userEmail: "bob@example.com",
	})
	// Another server's hook user must not be folded in.
	insertHookEvent(t, ctx, hookEventParams{
		projectID: projectID, deploymentID: deploymentID, timestamp: now.Add(-3 * time.Minute), traceID: uuid.New().String(),
		hookSource: "claude-code", toolSource: "plugin_acme-tools_Shipping", toolName: "quote", result: `"ok"`,
		userEmail: "carol@example.com",
	})

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	counts, err := ti.chClient.GetActiveCounts(ctx, telemetryRepo.GetActiveCountsParams{
		GramProjectID:        projectID,
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.UnixNano(),
		MCPServerID:          selected,
		MCPServerURLSuffixes: []string{"/mcp/acme-chat"},
		// The configured casing differs from alice's row; the normalized
		// variant beside it is what matches.
		ToolSources: []string{"External Acme Chat", "external_acme_chat"},
	})
	require.NoError(t, err)
	require.Equal(t, uint64(3), counts.ActiveUsersCount)

	// Without a hook identity the read keeps its gateway-only meaning.
	counts, err = ti.chClient.GetActiveCounts(ctx, telemetryRepo.GetActiveCountsParams{
		GramProjectID: projectID,
		TimeStart:     now.Add(-time.Hour).UnixNano(),
		TimeEnd:       now.UnixNano(),
		MCPServerID:   selected,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), counts.ActiveUsersCount)
}
