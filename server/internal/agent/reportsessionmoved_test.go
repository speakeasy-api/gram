package agent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// A per-user key records a chat_session:move audit event carrying the target
// harness and device attribution as metadata — and no session content.
func TestReportSessionMoved_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := seedCapturedChat(t, ti, sessionID, authCtx.UserID, "fix flaky auth test")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, &gen.ReportSessionMovedPayload{
		SessionID:       sessionID,
		TargetHarness:   "cursor",
		TargetSessionID: nil,
		SourceSurface:   conv.PtrEmpty("claude-code"),
		SerialNumber:    conv.PtrEmpty("C02XK1ABCDEF"),
		Hostname:        conv.PtrEmpty("dev-macbook-pro"),
		Email:           nil,
	}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, chatID.String(), rec.SubjectID)
	require.Equal(t, "fix flaky auth test", rec.SubjectDisplay)
	require.Equal(t, authCtx.UserID, rec.SubjectSlug, "owner user id rides in the subject slug")

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, "cursor", meta["target_harness"])
	require.Equal(t, "claude-code", meta["source_surface"])
	require.Equal(t, "c02xk1abcdef", meta["device_serial"], "serials normalize the way the sync path normalizes them")
	require.Equal(t, "dev-macbook-pro", meta["device_hostname"])
}

// The org install key may report moves (governance must work fleet-wide), but
// only with a vouched email to attribute the actor — mirroring getPlugins.
func TestReportSessionMoved_InstallKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	err := ti.service.ReportSessionMoved(ctx, &gen.ReportSessionMovedPayload{
		SessionID:       uuid.NewString(),
		TargetHarness:   "codex",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "email is required")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	require.NoError(t, ti.service.ReportSessionMoved(ctx, &gen.ReportSessionMovedPayload{
		SessionID:       uuid.NewString(),
		TargetHarness:   "codex",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           conv.PtrEmpty("dev@acme.corp"),
	}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

// A move reported before the session's hooks arrive is still recorded — the
// audit subject id is the deterministically derived chat id, so the entry
// lines up with the chat once capture catches up.
func TestReportSessionMoved_UncapturedSessionStillRecorded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	sessionID := "ses_" + uuid.NewString()
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, &gen.ReportSessionMovedPayload{
		SessionID:       sessionID,
		TargetHarness:   "claude-code",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, chat.SessionIDToChatID(sessionID).String(), rec.SubjectID)
	require.Empty(t, rec.SubjectDisplay, "no title yet — the chat has not been captured")
}

// A blank target harness is rejected before anything is written: Goa's
// Required only checks presence, and a move record with no destination is
// useless for governance.
func TestReportSessionMoved_BlankTargetHarnessRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	err = ti.service.ReportSessionMoved(userCtx, &gen.ReportSessionMovedPayload{
		SessionID:       uuid.NewString(),
		TargetHarness:   "   ",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "target_harness is required")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// Flag off: no audit rows, no metadata — the surface is dark.
func TestReportSessionMoved_FeatureDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	ti.features.sessionPortability = false

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	err = ti.service.ReportSessionMoved(ctx, &gen.ReportSessionMovedPayload{
		SessionID:       uuid.NewString(),
		TargetHarness:   "cursor",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           conv.PtrEmpty("dev@acme.corp"),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "not enabled")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// fetchSessionLinks reads lineage edges through the chat package's
// sqlc-generated query — the same read path the dashboard's detail panel uses,
// so these tests exercise write and read as one contract.
func fetchSessionLinks(t *testing.T, ctx context.Context, ti *testInstance, parentChatID uuid.UUID) []chatrepo.ListChatSessionLinksRow {
	t.Helper()
	rows, err := chatrepo.New(ti.conn).ListChatSessionLinks(ctx, chatrepo.ListChatSessionLinksParams{
		ProjectID:      ti.projectID,
		ChatIds:        []uuid.UUID{parentChatID},
		ExternalUserID: "",
		UserID:         "",
	})
	require.NoError(t, err)
	return rows
}

// A move that knows its continuation's session id records a complete lineage
// edge — both chat ids derived the same way hook ingest derives them — and the
// audit metadata carries the target session id.
func TestReportSessionMoved_RecordsLineageEdge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	sessionID := uuid.NewString()
	targetSessionID := uuid.NewString()
	parentChatID := chat.SessionIDToChatID(sessionID)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, &gen.ReportSessionMovedPayload{
		SessionID:       sessionID,
		TargetHarness:   "claude-code",
		TargetSessionID: conv.PtrEmpty(targetSessionID),
		SourceSurface:   conv.PtrEmpty("claude-code"),
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	}))

	rows := fetchSessionLinks(t, ctx, ti, parentChatID)
	require.Len(t, rows, 1)
	row := rows[0]
	require.True(t, row.ChildChatID.Valid)
	require.Equal(t, chat.SessionIDToChatID(targetSessionID), row.ChildChatID.UUID)
	require.True(t, row.ChildSessionID.Valid)
	require.Equal(t, targetSessionID, row.ChildSessionID.String)
	require.True(t, row.ActorEmail.Valid)
	require.Equal(t, "dev@acme.corp", row.ActorEmail.String)
	require.Equal(t, "claude-code", row.TargetHarness)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, targetSessionID, meta["target_session_id"])
}

// A move whose continuation id is unknowable (Cursor mints ids server-side)
// still records a dangling edge: parent side filled in, child side NULL.
func TestReportSessionMoved_UnknownContinuationRecordsDanglingEdge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	sessionID := uuid.NewString()
	parentChatID := chat.SessionIDToChatID(sessionID)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, &gen.ReportSessionMovedPayload{
		SessionID:       sessionID,
		TargetHarness:   "cursor",
		TargetSessionID: nil,
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	}))

	rows := fetchSessionLinks(t, ctx, ti, parentChatID)
	require.Len(t, rows, 1)
	require.False(t, rows[0].ChildChatID.Valid, "unknowable continuation leaves the child NULL")
	require.False(t, rows[0].ChildSessionID.Valid)
	require.Equal(t, "cursor", rows[0].TargetHarness)
}

// The daemon fires move reports fire-and-forget and may retry: a repeated
// report of the same known continuation must not double-edge, while each
// report still lands its own governance audit event.
func TestReportSessionMoved_RetryDoesNotDoubleEdge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	sessionID := uuid.NewString()
	parentChatID := chat.SessionIDToChatID(sessionID)
	payload := &gen.ReportSessionMovedPayload{
		SessionID:       sessionID,
		TargetHarness:   "claude-code",
		TargetSessionID: conv.PtrEmpty(uuid.NewString()),
		SourceSurface:   nil,
		SerialNumber:    nil,
		Hostname:        nil,
		Email:           nil,
	}

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)

	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, payload))
	require.NoError(t, ti.service.ReportSessionMoved(userCtx, payload))

	require.Len(t, fetchSessionLinks(t, ctx, ti, parentChatID), 1, "retries dedupe on the partial unique index")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionMove)
	require.NoError(t, err)
	require.Equal(t, before+2, after, "every report is its own governance event")
}
