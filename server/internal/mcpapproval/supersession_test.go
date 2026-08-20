package mcpapproval_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// reviewInTx runs the URL-edit review in its own committed transaction, the
// way the risk update handler does.
func reviewInTx(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	policyID uuid.UUID,
	disposition string,
	desiredAllowed []string,
	desiredBlocked []string,
) shadowmcp.StandingDecisionReview {
	t.Helper()

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	review, err := ti.service.ReviewShadowMCPPolicyURLEdit(ctx, tx, ti.organizationID, ti.projectID, policyID, disposition, desiredAllowed, desiredBlocked)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return review
}

// Unchecking an approved server from a block_all policy's allow list is a
// contradiction the edit must confirm; re-sending the list with the server
// still on it is not.
func TestReviewShadowMCPPolicyURLEdit_FlagsRemovalOfApprovedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/supersede-removal"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	retained := reviewInTx(t, ctx, ti, policyID, "block_all", []string{serverURL}, nil)
	require.Empty(t, retained.Conflicts)
	require.Equal(t, []string{serverURL}, retained.StandingURLs)

	removed := reviewInTx(t, ctx, ti, policyID, "block_all", []string{}, nil)
	require.Len(t, removed.Conflicts, 1)
	require.Equal(t, requestID, removed.Conflicts[0].RequestID)
	require.Equal(t, serverURL, removed.Conflicts[0].TargetKey)
	require.Equal(t, "approved", removed.Conflicts[0].Decision)
}

// Allow-listing a denied server contradicts the denial; an audience-only
// save (no list sent) can never conflict because membership is unchanged.
func TestReviewShadowMCPPolicyURLEdit_FlagsAdditionOfDeniedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/supersede-addition"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)

	added := reviewInTx(t, ctx, ti, policyID, "block_all", []string{serverURL}, nil)
	require.Len(t, added.Conflicts, 1)
	require.Equal(t, "denied", added.Conflicts[0].Decision)

	audienceOnly := reviewInTx(t, ctx, ti, policyID, "block_all", nil, nil)
	require.Empty(t, audienceOnly.Conflicts)
	require.Equal(t, []string{serverURL}, audienceOnly.StandingURLs)
}

// On an allow_all policy's block list the directions invert: removing a
// denied server's block and block-listing an approved server both
// contradict standing decisions.
func TestReviewShadowMCPPolicyURLEdit_AllowAllInvertsDirections(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "allow_all")
	deniedURL := "https://mcp.example.com/allow-all-denied"
	approvedURL := "https://mcp.example.com/allow-all-approved"
	deniedID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: deniedURL, status: "requested", evidence: "", version: 0})
	approvedID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: approvedURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(deniedID.String(), "denied"))
	require.NoError(t, err)
	_, err = ti.service.RecordDecision(ctx, decisionPayload(approvedID.String(), "approved"))
	require.NoError(t, err)

	// The denial wrote the block rule, so the current block list is exactly
	// the denied URL. Dropping it and block-listing the approved server both
	// conflict in one edit.
	review := reviewInTx(t, ctx, ti, policyID, "allow_all", nil, []string{approvedURL})
	require.Len(t, review.Conflicts, 2)
	require.Equal(t, approvedURL, review.Conflicts[0].TargetKey)
	require.Equal(t, "approved", review.Conflicts[0].Decision)
	require.Equal(t, deniedURL, review.Conflicts[1].TargetKey)
	require.Equal(t, "denied", review.Conflicts[1].Decision)

	// Keeping the block list as it stands conflicts with nothing.
	unchanged := reviewInTx(t, ctx, ti, policyID, "allow_all", nil, []string{deniedURL})
	require.Empty(t, unchanged.Conflicts)
}

// Superseding transitions the request, audits the displacement, and removes
// the decision from the standing set — later reviews and replays no longer
// see it.
func TestSupersedeShadowMCPDecisions_TransitionsAndAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/superseded"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	review, err := ti.service.ReviewShadowMCPPolicyURLEdit(ctx, tx, ti.organizationID, ti.projectID, policyID, "block_all", []string{}, nil)
	require.NoError(t, err)
	require.Len(t, review.Conflicts, 1)

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "user-policy-editor")
	require.NoError(t, ti.service.SupersedeShadowMCPDecisions(ctx, tx, ti.organizationID, ti.projectID, review.Conflicts, actor, nil))
	require.NoError(t, tx.Commit(ctx))

	require.Equal(t, "superseded", requestStatus(t, ctx, ti, ti.projectID, requestID))

	var audited int
	require.NoError(t, ti.conn.QueryRow(ctx,
		"SELECT count(*) FROM audit_logs WHERE action = 'mcp_approval_request:supersede' AND subject_id = $1 AND actor_id = $2",
		requestID.String(), "user-policy-editor").Scan(&audited))
	require.Equal(t, 1, audited)

	after := reviewInTx(t, ctx, ti, policyID, "block_all", []string{}, nil)
	require.Empty(t, after.Conflicts)
	require.Empty(t, after.StandingURLs)
}

// Superseded is not a dead end: recording a new decision moves the request
// back into the ordinary lifecycle and re-derives enforcement from the new
// decision — the one sanctioned way to restore access a policy edit
// displaced.
func TestRecordDecision_ReDecideRestoresSupersededRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/re-decided"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	// A confirmed policy edit displaces the approval and removes its grant.
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	review, err := ti.service.ReviewShadowMCPPolicyURLEdit(ctx, tx, ti.organizationID, ti.projectID, policyID, "block_all", []string{}, nil)
	require.NoError(t, err)
	require.Len(t, review.Conflicts, 1)
	require.NoError(t, ti.service.SupersedeShadowMCPDecisions(ctx, tx, ti.organizationID, ti.projectID, review.Conflicts, urn.NewPrincipal(urn.PrincipalTypeUser, "user-policy-editor"), nil))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, policybypass.RevokePolicyURLGrantVariants(ctx, ti.conn, ti.organizationID, authz.ScopeRiskPolicyBypass, policyID.String(), serverURL))
	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	// Re-deciding writes fresh grants and the request reads approved again;
	// the review is standing once more.
	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	standing := reviewInTx(t, ctx, ti, policyID, "block_all", nil, nil)
	require.Equal(t, []string{serverURL}, standing.StandingURLs)
}

// A fresh ask for a superseded server reopens the review to requested — the
// displaced decision does not silently answer new demand.
func TestUpsertApprovalRequest_ReopensSupersededRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/reopened"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "superseded", evidence: "", version: 0})

	row, err := ti.repo.UpsertApprovalRequest(ctx, repo.UpsertApprovalRequestParams{
		OrganizationID:            ti.organizationID,
		ProjectID:                 ti.projectID,
		TargetKind:                "server_url",
		TargetRaw:                 serverURL,
		TargetKey:                 serverURL,
		ArtifactRef:               conv.ToPGTextEmpty(""),
		VersionPinned:             false,
		Status:                    "requested",
		RiskPolicyBypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	require.Equal(t, requestID, row.ID)
	require.Equal(t, "requested", row.Status)
}

// The policy replay never resurrects a superseded decision: creating a new
// blocking policy backfills standing decisions only.
func TestReconcileStandingDecisions_SkipsSuperseded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Both decisions are recorded while no blocking policy exists, so
	// neither writes grants yet; one is then superseded.
	supersededURL := "https://mcp.example.com/dormant-superseded"
	standingURL := "https://mcp.example.com/dormant-standing"
	supersededID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: supersededURL, status: "requested", evidence: "", version: 0})
	standingID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: standingURL, status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(supersededID.String(), "approved"))
	require.NoError(t, err)
	_, err = ti.service.RecordDecision(ctx, decisionPayload(standingID.String(), "approved"))
	require.NoError(t, err)

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "user-policy-editor")
	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	require.NoError(t, ti.service.SupersedeShadowMCPDecisions(ctx, tx, ti.organizationID, ti.projectID, []shadowmcp.StandingDecisionConflict{{
		RequestID: supersededID,
		TargetKey: supersededURL,
		TargetRaw: supersededURL,
		Decision:  "approved",
	}}, actor, nil))
	require.NoError(t, tx.Commit(ctx))

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	replayTx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = replayTx.Rollback(ctx) }()
	require.NoError(t, ti.service.ReconcileStandingDecisionsForPolicy(ctx, replayTx, ti.organizationID, ti.projectID, policyID))
	require.NoError(t, replayTx.Commit(ctx))

	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, supersededURL))
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, standingURL))
}
