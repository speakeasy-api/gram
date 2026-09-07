package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// Bypass rows are per-requester: one blocked server accumulates one row per
// blocked person, and only one of them is ever linked to the review through a
// promotion. The decision answers all of them, so every still-requested row
// for the same canonical URL resolves with it — none may stay pending on the
// legacy queue and the inventory's request counters.
func TestRecordDecision_DrainsAllPendingBypassRowsForServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/multi-ask"
	firstAsk := seedBypassRequest(t, ctx, ti, ti.projectID, serverURL, "user-one", "first ask")
	// The second ask spells the same server differently; canonical URL is
	// what the drain keys on.
	secondAsk := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com:443/multi-ask", "user-two", "second ask")

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", serverURL, "proactive review"))
	require.NoError(t, err)

	approveCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestApprove)
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(created.ID, "approved"))
	require.NoError(t, err)

	allUsers := authz.AllUsersPrincipal().String()
	for _, bypassID := range []uuid.UUID{firstAsk, secondAsk} {
		row, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
			ID:        bypassID,
			ProjectID: ti.projectID,
		})
		require.NoError(t, err)
		require.Equal(t, "approved", row.Status)
		require.True(t, row.DecidedBy.Valid)
		require.Equal(t, []string{allUsers}, row.GrantedPrincipalUrns)
	}

	// Each drained row audits its own transition.
	approveCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestApprove)
	require.NoError(t, err)
	require.Equal(t, approveCountBefore+2, approveCountAfter)
}

// The drained row's status transition is audited exactly the way the legacy
// approve/deny endpoints audit it — before/after snapshots, metadata, the
// policy as the subject — with the decider as the actor.
func TestRecordDecision_AuditsDrainedBypassResolution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/audited-drain"
	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, serverURL, "blocked-user", "hit the block")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)

	denyCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(promoted.ID, "denied"))
	require.NoError(t, err)

	denyCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)
	require.Equal(t, denyCountBefore+1, denyCountAfter)

	bypassRow, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
		ID:        bypassID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "denied", bypassRow.Status)

	policy, err := riskrepo.New(ti.conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        bypassRow.RiskPolicyID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)
	require.Equal(t, policy.Name, record.SubjectDisplay)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, bypassID.String(), metadata["request_id"])
	require.Equal(t, "requested", metadata["previous_status"])
	require.Equal(t, "denied", metadata["current_status"])

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "requested", beforeSnapshot["status"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "denied", afterSnapshot["status"])
	require.Equal(t, ti.authContext.UserID, afterSnapshot["decided_by"])
}

// A row already decided through the legacy queue keeps its recorded outcome:
// the drain's status guard must not rewrite history, and no audit event is
// emitted for a transition that did not happen.
func TestRecordDecision_DrainKeepsAlreadyDecidedRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/already-decided"
	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, serverURL, "blocked-user", "old ask")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)

	// The legacy queue decided this row first.
	_, err = riskrepo.New(ti.conn).UpdateRiskPolicyBypassRequestStatus(ctx, riskrepo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               "approved",
		DecidedBy:            conv.ToPGText("legacy-admin"),
		GrantedPrincipalUrns: []string{},
		ID:                   bypassID,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)

	denyCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(promoted.ID, "denied"))
	require.NoError(t, err)

	row, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
		ID:        bypassID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", row.Status, "the legacy queue's outcome stands")

	denyCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyBypassRequestDeny)
	require.NoError(t, err)
	require.Equal(t, denyCountBefore, denyCountAfter, "no audit for a transition that did not happen")
}
