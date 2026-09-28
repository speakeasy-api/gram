package telemetry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// waitForSupportCoverage polls until the seeded rows have propagated through
// the session-summary materialized view, which is eventually consistent.
func waitForSupportCoverage(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	ready func(*telemetry.SupportCoverageResult) bool,
) *telemetry.SupportCoverageResult {
	t.Helper()

	var result *telemetry.SupportCoverageResult
	var err error
	require.Eventually(t, func() bool {
		result, err = ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
		return err == nil && result != nil && ready(result)
	}, 10*time.Second, 200*time.Millisecond, "expected support coverage to become query-ready")
	// After Eventually, not inside it: the condition runs in its own
	// goroutine, so a require there would not fail fast.
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	return result
}

// cellFor finds the cell for a pair. Every pair is promised, so a miss fails.
func cellFor(t *testing.T, result *telemetry.SupportCoverageResult, capability, surface string) telemetry.SupportCoverageCell {
	t.Helper()

	for _, cell := range result.Cells {
		if cell.Capability == capability && cell.Surface == surface {
			return cell
		}
	}
	t.Fatalf("no cell for capability %q surface %q", capability, surface)
	return telemetry.SupportCoverageCell{} //exhaustruct:ignore
}

func TestGetSupportCoverage_EmptyOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	// Always complete, so a client never has to interpret a missing cell.
	require.Len(t, result.Cells, 5*7, "every capability/surface pair is returned")
	require.Equal(t, 30, result.WindowDays)

	require.Equal(t, "none", cellFor(t, result, "session", "claude_code").Status)
	require.Equal(t, "none", cellFor(t, result, "identity", "claude_code").Status)

	require.Equal(t, "none", cellFor(t, result, "shadow", "claude_code").Status)

	require.Equal(t, "none", cellFor(t, result, "session", "mcp_gateway").Status)
	require.Equal(t, "none", cellFor(t, result, "blocking", "mcp_gateway").Status)
}

// The gateway can never report tokens or shadow servers, and an operator must
// not read either cell as a gap an integration could close.
func TestGetSupportCoverage_GatewayReportsInapplicablePairs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	cost := cellFor(t, result, "cost", "mcp_gateway")
	require.Equal(t, "na", cost.Status)
	require.NotEmpty(t, cost.Detail, "an inapplicable cell has to say why")

	shadow := cellFor(t, result, "shadow", "mcp_gateway")
	require.Equal(t, "na", shadow.Status)
	require.NotEmpty(t, shadow.Detail)

	// Only the gateway has inapplicable pairs; an agent surface with no
	// activity is a genuine gap.
	require.Equal(t, "none", cellFor(t, result, "cost", "cursor").Status)
}

func TestGetSupportCoverage_GatewayTrafficAndIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// Three calls through Gram-hosted MCP: one carrying a person, one a
	// managed agent, one anonymous. Plus one gateway-endpoint dispatch.
	insertGatewayToolCall(t, ctx, ti.projectID, gatewayCall{
		toolsetSlug: "sales", userEmail: "person@example.com", seenAt: now,
	})
	insertGatewayToolCall(t, ctx, ti.projectID, gatewayCall{
		toolsetSlug: "sales", agentID: "agent-1", seenAt: now,
	})
	insertGatewayToolCall(t, ctx, ti.projectID, gatewayCall{
		toolsetSlug: "sales", seenAt: now,
	})
	insertGatewayToolCall(t, ctx, ti.projectID, gatewayCall{
		metaMCPServerID: uuid.NewString(), userID: "user-1", seenAt: now,
	})

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	session := cellFor(t, result, "session", "mcp_gateway")
	require.Equal(t, "observed", session.Status)
	require.Equal(t, int64(4), session.Value)
	// The gateway serves calls, not sessions, so it names its own unit rather
	// than inheriting the row's.
	require.Equal(t, "tool call", session.Unit)
	require.False(t, session.LastSeen.IsZero())

	// Only the emailed call and the user-id call are bound to a person; the
	// agent-authenticated one is stated separately rather than summed in.
	identity := cellFor(t, result, "identity", "mcp_gateway")
	require.Equal(t, "observed", identity.Status)
	require.Equal(t, int64(2), identity.Value)
	require.Contains(t, identity.Detail, "managed agent")

	// Gateway traffic carries no hook_source, so it must not appear as an
	// agent surface or as an unmapped source.
	require.Equal(t, "none", cellFor(t, result, "session", "other").Status)
	require.Empty(t, result.Unmapped)
}

func TestGetSupportCoverage_GatewayIgnoresHookTraffic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// A hook report about an MCP call is the agent's observation of it, not
	// the gateway serving it.
	insertGatewayToolCall(t, ctx, ti.projectID, gatewayCall{
		toolsetSlug: "sales", eventSource: "hook", hookSource: "cursor", seenAt: now,
	})

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	require.Equal(t, "none", cellFor(t, result, "session", "mcp_gateway").Status)
}

func TestGetSupportCoverage_GatewayPolicyEnforcement(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// Two rules fired on one denied call: that stopped one call, not two.
	deniedExecution := uuid.NewString()
	insertMediatedFinding(t, ctx, ti.orgID, mediatedFinding{
		executionID: deniedExecution, outcome: "denied", createdAt: now,
	})
	insertMediatedFinding(t, ctx, ti.orgID, mediatedFinding{
		executionID: deniedExecution, outcome: "denied", createdAt: now,
	})
	insertMediatedFinding(t, ctx, ti.orgID, mediatedFinding{
		executionID: uuid.NewString(), outcome: "logged", createdAt: now,
	})

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	blocking := cellFor(t, result, "blocking", "mcp_gateway")
	require.Equal(t, "observed", blocking.Status)
	require.Equal(t, int64(1), blocking.Value)
	require.Equal(t, "block", blocking.Unit)
	require.False(t, blocking.LastSeen.IsZero())

	// Enforcement on the gateway is not enforcement on an agent surface.
	require.Equal(t, "none", cellFor(t, result, "blocking", "cursor").Status)
}

func TestGetSupportCoverage_GatewayScannedButNeverStopped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	insertMediatedFinding(t, ctx, ti.orgID, mediatedFinding{
		executionID: uuid.NewString(), outcome: "logged", createdAt: now,
	})

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	// The seam demonstrably ran, which is different from no policy being
	// configured at all, so it does not read as an empty cell.
	blocking := cellFor(t, result, "blocking", "mcp_gateway")
	require.Equal(t, "pending", blocking.Status)
	require.Contains(t, blocking.Detail, "none stopped")
	require.True(t, blocking.LastSeen.IsZero())
}

func TestGetSupportCoverage_GatewayIgnoresChatOnlyFindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// A finding from chat-message scanning carries no mediation surface: it
	// is not evidence that the gateway enforced anything.
	insertMediatedFinding(t, ctx, ti.orgID, mediatedFinding{
		executionID: uuid.NewString(), outcome: "denied", chatOnly: true, createdAt: now,
	})

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	require.Equal(t, "none", cellFor(t, result, "blocking", "mcp_gateway").Status)
}

func TestGetSupportCoverage_SessionAndIdentityEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	projectID := uuid.MustParse(ti.projectID)
	now := time.Now().UTC().Add(-time.Hour)

	// Two claude-code sessions: one carrying a user email, one carrying
	// neither an email nor a hostname.
	attributedChat := uuid.NewString()
	insertSessionChat(t, ctx, ti, attributedChat, projectID, ti.orgID, "attributed")
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		chatID: attributedChat, projectID: ti.projectID, timestamp: now,
		email: "person@example.com", hookSource: "cursor", model: "sonnet",
		inputTokens: 10, outputTokens: 5, totalTokens: 15,
	})

	anonymousChat := uuid.NewString()
	insertSessionChat(t, ctx, ti, anonymousChat, projectID, ti.orgID, "anonymous")
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		chatID: anonymousChat, projectID: ti.projectID, timestamp: now,
		email: "", hookSource: "cursor", model: "sonnet",
		inputTokens: 20, outputTokens: 5, totalTokens: 25,
	})

	result := waitForSupportCoverage(t, ctx, ti, func(res *telemetry.SupportCoverageResult) bool {
		for _, cell := range res.Cells {
			if cell.Capability == "session" && cell.Surface == "cursor" && cell.Value == 2 {
				return true
			}
		}
		return false
	})

	session := cellFor(t, result, "session", "cursor")
	require.Equal(t, "observed", session.Status)
	require.Equal(t, int64(2), session.Value)
	require.False(t, session.LastSeen.IsZero())

	cost := cellFor(t, result, "cost", "cursor")
	require.Equal(t, "observed", cost.Status)
	require.Equal(t, int64(40), cost.Value)

	// Only the emailed session is bound to a person.
	identity := cellFor(t, result, "identity", "cursor")
	require.Equal(t, "observed", identity.Status)
	require.Equal(t, int64(1), identity.Value)

	// A surface with no activity stays empty rather than inheriting another's.
	require.Equal(t, "none", cellFor(t, result, "session", "codex").Status)
}

func TestGetSupportCoverage_ReportsUnmappedHookSources(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// Traces carry whatever hook_source was reported, so a new adapter shows
	// up before the fold knows it. The session-summary MV cannot stand in:
	// it only ingests a known set of sources.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://unknown.example.com/mcp", "some-brand-new-agent", now)

	result := waitForSupportCoverage(t, ctx, ti, func(res *telemetry.SupportCoverageResult) bool {
		return len(res.Unmapped) == 1
	})

	// The old client-side fold dropped unrecognized sources silently, so the
	// activity vanished from the matrix while the summary still read as full
	// coverage. It has to come back as an explicit report instead.
	require.Equal(t, "some-brand-new-agent", result.Unmapped[0].HookSource)

	for _, surface := range []string{"claude_code", "cursor", "other"} {
		require.Equal(t, "none", cellFor(t, result, "shadow", surface).Status,
			"unmapped activity must not be folded into a surface")
	}
}

func TestGetSupportCoverage_ShadowExposurePerSurface(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "cursor", now)
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://other.example.com/mcp", "cursor", now)
	// The same server reached twice by one surface is one server, not two.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "cursor", now.Add(time.Minute))

	result, err := ti.coverage.SupportCoverageForOrganization(ctx, ti.orgID, 30)
	require.NoError(t, err)

	shadow := cellFor(t, result, "shadow", "cursor")
	require.Equal(t, "observed", shadow.Status)
	require.Equal(t, int64(2), shadow.Value)

	// Derived from traces, which cover the whole retention window, so a
	// surface with no shadow traffic is genuinely clear.
	require.Equal(t, "none", cellFor(t, result, "shadow", "codex").Status)
}

func TestGetSupportCoverage_FoldsShadowAliasesToOneServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	now := time.Now().UTC().Add(-time.Hour)

	// Two spellings of one surface reaching the same server is one server.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "claude-code", now)
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "claudecode", now)

	result := waitForSupportCoverage(t, ctx, ti, func(res *telemetry.SupportCoverageResult) bool {
		return cellStatus(res, "shadow", "claude_code") == "observed"
	})

	require.Equal(t, int64(1), cellFor(t, result, "shadow", "claude_code").Value)
}

func cellStatus(result *telemetry.SupportCoverageResult, capability, surface string) string {
	for _, cell := range result.Cells {
		if cell.Capability == capability && cell.Surface == surface {
			return cell.Status
		}
	}
	return ""
}

// gatewayCall is one trace as the MCP gateway would have written it.
// eventSource defaults to the tool-call value the gateway stamps; a test sets
// it to "hook" to write an agent-side observation instead.
type gatewayCall struct {
	toolsetSlug     string
	metaMCPServerID string
	eventSource     string
	hookSource      string
	userEmail       string
	userID          string
	agentID         string
	seenAt          time.Time
}

func insertGatewayToolCall(t *testing.T, ctx context.Context, projectID string, call gatewayCall) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	eventSource := call.eventSource
	if eventSource == "" {
		eventSource = "tool_call"
	}

	traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	err = conn.Exec(ctx, `
		INSERT INTO trace_summaries
			(gram_project_id, trace_id, event_source, hook_source, toolset_slug,
			 meta_mcp_server_id, user_email, user_id, agent_id, start_time_unix_nano)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, projectID, traceID, eventSource, call.hookSource, call.toolsetSlug,
		call.metaMCPServerID, call.userEmail, call.userID, call.agentID, call.seenAt.UnixNano())
	require.NoError(t, err)
}

// mediatedFinding is one risk finding as a mediation seam would have written
// it. It defaults to the hosted MCP seam; chatOnly writes the empty mediation
// surface a chat-message scan produces instead.
type mediatedFinding struct {
	executionID string
	outcome     string
	chatOnly    bool
	createdAt   time.Time
}

func insertMediatedFinding(t *testing.T, ctx context.Context, orgID string, finding mediatedFinding) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	surface := "hosted_mcp"
	if finding.chatOnly {
		surface = ""
	}

	err = conn.Exec(ctx, `
		INSERT INTO risk_findings
			(id, created_at, organization_id, rule_id, source, mediation_surface,
			 mcp_method, enforcement_outcome, execution_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.New(), finding.createdAt, orgID, "pii.email_address", "presidio",
		surface, "tools/call", finding.outcome, finding.executionID)
	require.NoError(t, err)
}

// insertShadowSurfaceSighting writes the two rows coverage derives the
// (server, surface) pair from: an inventory entry and a matching trace.
func insertShadowSurfaceSighting(t *testing.T, ctx context.Context, projectID, serverURL, hookSource string, seenAt time.Time) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO shadow_mcp_inventory_urls
			(gram_project_id, canonical_server_url, url_host, server_name, first_seen, last_seen, updated_at)
		VALUES (?, ?, ?, ?, fromUnixTimestamp64Nano(?), fromUnixTimestamp64Nano(?), fromUnixTimestamp64Nano(?))
	`, projectID, serverURL, "example.com", "shadow", seenAt.UnixNano(), seenAt.UnixNano(), seenAt.UnixNano())
	require.NoError(t, err)

	traceID := strings.ReplaceAll(uuid.NewString(), "-", "")
	err = conn.Exec(ctx, `
		INSERT INTO trace_summaries
			(gram_project_id, trace_id, hook_source, mcp_server_url, start_time_unix_nano)
		VALUES (?, ?, ?, ?, ?)
	`, projectID, traceID, hookSource, serverURL, seenAt.UnixNano())
	require.NoError(t, err)
}
