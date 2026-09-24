package telemetry_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// platformAdminCtx marks the caller as Speakeasy staff, which the handler
// requires.
func platformAdminCtx(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	return contextvalues.SetAuthContext(ctx, &platformAuth)
}

// waitForSupportCoverage polls until the seeded rows have propagated through
// the session-summary materialized view, which is eventually consistent.
func waitForSupportCoverage(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	ready func(*telem_gen.SupportCoverageResult) bool,
) *telem_gen.SupportCoverageResult {
	t.Helper()

	var result *telem_gen.SupportCoverageResult
	var err error
	require.Eventually(t, func() bool {
		result, err = ti.service.GetSupportCoverage(ctx, &telem_gen.GetSupportCoveragePayload{
			SessionToken: nil, WindowDays: 30,
		})
		return err == nil && result != nil && ready(result)
	}, 10*time.Second, 200*time.Millisecond, "expected support coverage to become query-ready")
	// After Eventually, not inside it: the condition runs in its own
	// goroutine, so a require there would not fail fast.
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	return result
}

// cellFor finds the cell for a pair. Every pair is promised, so a miss fails.
func cellFor(t *testing.T, result *telem_gen.SupportCoverageResult, capability, surface string) *telem_gen.SupportCoverageCell {
	t.Helper()

	for _, cell := range result.Cells {
		if cell.Capability == capability && cell.Surface == surface {
			return cell
		}
	}
	t.Fatalf("no cell for capability %q surface %q", capability, surface)
	return nil
}

func TestGetSupportCoverage_RefusesNonPlatformAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	// Org membership is not enough; PlatformAdminGate is presentation only.
	_, err := ti.service.GetSupportCoverage(ctx, &telem_gen.GetSupportCoveragePayload{
		SessionToken: nil, WindowDays: 30,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "platform admin")
}

func TestGetSupportCoverage_EmptyOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = platformAdminCtx(t, ctx)

	result, err := ti.service.GetSupportCoverage(ctx, &telem_gen.GetSupportCoveragePayload{
		SessionToken: nil, WindowDays: 30,
	})
	require.NoError(t, err)

	// Always complete, so a client never has to interpret a missing cell.
	require.Len(t, result.Cells, 5*6, "every capability/surface pair is returned")
	require.Equal(t, 30, result.WindowDays)

	require.Equal(t, "none", cellFor(t, result, "session", "claude_code").Status)
	require.Equal(t, "none", cellFor(t, result, "identity", "claude_code").Status)

	require.Equal(t, "none", cellFor(t, result, "shadow", "claude_code").Status)
}

func TestGetSupportCoverage_SessionAndIdentityEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = platformAdminCtx(t, ctx)
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

	result := waitForSupportCoverage(t, ctx, ti, func(res *telem_gen.SupportCoverageResult) bool {
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
	require.NotEmpty(t, session.LastSeen)

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
	ctx = platformAdminCtx(t, ctx)
	now := time.Now().UTC().Add(-time.Hour)

	// Traces carry whatever hook_source was reported, so a new adapter shows
	// up before the fold knows it. The session-summary MV cannot stand in:
	// it only ingests a known set of sources.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://unknown.example.com/mcp", "some-brand-new-agent", now)

	result := waitForSupportCoverage(t, ctx, ti, func(res *telem_gen.SupportCoverageResult) bool {
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
	ctx = platformAdminCtx(t, ctx)
	now := time.Now().UTC().Add(-time.Hour)

	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "cursor", now)
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://other.example.com/mcp", "cursor", now)
	// The same server reached twice by one surface is one server, not two.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "cursor", now.Add(time.Minute))

	result, err := ti.service.GetSupportCoverage(ctx, &telem_gen.GetSupportCoveragePayload{
		SessionToken: nil, WindowDays: 30,
	})
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
	ctx = platformAdminCtx(t, ctx)
	now := time.Now().UTC().Add(-time.Hour)

	// Two spellings of one surface reaching the same server is one server.
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "claude-code", now)
	insertShadowSurfaceSighting(t, ctx, ti.projectID, "https://evil.example.com/mcp", "claudecode", now)

	result := waitForSupportCoverage(t, ctx, ti, func(res *telem_gen.SupportCoverageResult) bool {
		return cellStatus(res, "shadow", "claude_code") == "observed"
	})

	require.Equal(t, int64(1), cellFor(t, result, "shadow", "claude_code").Value)
}

func cellStatus(result *telem_gen.SupportCoverageResult, capability, surface string) string {
	for _, cell := range result.Cells {
		if cell.Capability == capability && cell.Surface == surface {
			return cell.Status
		}
	}
	return ""
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
