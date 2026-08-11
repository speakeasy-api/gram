package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func decisionPayload(id, decision string) *gen.RecordDecisionPayload {
	return &gen.RecordDecisionPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		ID: id, Decision: decision, Rationale: "recorded in a test", GrantedPrincipalUrns: nil,
	}
}

func TestRecordDecision_Approve(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	principal := seedMemberPrincipal(t, ctx, ti, "user-platform-lead")
	payload := decisionPayload(requestID.String(), "approved")
	payload.Rationale = "read-only tools, pinned version, vendor we already use"
	payload.GrantedPrincipalUrns = []string{principal}

	decision, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "approved", decision.Decision)
	require.NotEmpty(t, decision.DecidedBy)
	require.NotNil(t, decision.Rationale)
	require.Equal(t, []string{principal}, decision.GrantedPrincipalUrns)

	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// A denial grants nobody anything, whatever the caller sent alongside it.
func TestRecordDecision_DenyDropsGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "denied")
	payload.Rationale = "demands a broad token and publishes no source"
	payload.GrantedPrincipalUrns = []string{"role:platform", "urn:gram:user:someone"}

	decision, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "denied", decision.Decision)
	require.Empty(t, decision.GrantedPrincipalUrns)
	require.Equal(t, "denied", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// The evidence a reviewer actually saw is frozen onto the decision, so a later
// re-gather cannot rewrite the record of what was decided on.
func TestRecordDecision_FreezesEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	evidence := `{"authority": {"mode": "api_key"}}`
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested", evidence: evidence, version: 7,
	})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	frozen := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, frozen, 1)
	require.JSONEq(t, evidence, string(frozen[0].EvidenceSnapshot))

	// The version is copied from the request rather than defaulted, or the
	// frozen payload would later be read against the wrong shape.
	require.Equal(t, int32(7), frozen[0].EvidenceVersion)

	// Re-gathering the request's evidence leaves the decision untouched.
	seedEvidence(t, ctx, ti, ti.projectID, requestID, `{"authority":{"mode":"none"}}`, 8)

	after := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, after, 1)
	require.JSONEq(t, evidence, string(after[0].EvidenceSnapshot))
	require.Equal(t, int32(7), after[0].EvidenceVersion)
}

// Decisions accumulate rather than replace, so "have we decided on this
// before?" is answerable from the row itself.
func TestRecordDecision_AccumulatesHistory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	first := decisionPayload(requestID.String(), "denied")
	first.Rationale = "no pinned version"
	_, err := ti.service.RecordDecision(ctx, first)
	require.NoError(t, err)

	second := decisionPayload(requestID.String(), "approved")
	second.Rationale = "now pinned"
	_, err = ti.service.RecordDecision(ctx, second)
	require.NoError(t, err)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Len(t, detail.Decisions, 2)

	// Newest first, so a repeat request starts from the last rationale.
	require.Equal(t, "approved", detail.Decisions[0].Decision)
	require.Equal(t, "denied", detail.Decisions[1].Decision)
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// Deciding on another project's request must fail before anything is written,
// even though the caller holds the decide scope in their own project.
func TestRecordDecision_OtherProjectIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(theirs.String(), "approved"))
	requireOopsCode(t, err, oops.CodeNotFound)

	// Nothing was written, and their request is untouched.
	require.Equal(t, "requested", requestStatus(t, ctx, ti, otherProject, theirs))
	require.Empty(t, decisionsFor(t, ctx, ti, otherProject, theirs))
}

// The organisation on a decision is derived from the request it decides, not
// from the caller's active organisation, so the two can never disagree.
func TestRecordDecision_OrganizationFollowsRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	request, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{
		ID: requestID, ProjectID: ti.projectID,
	})
	require.NoError(t, err)

	decisions := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, decisions, 1)
	require.Equal(t, request.OrganizationID, decisions[0].OrganizationID)
}

// A bypass request already decided through the legacy drain keeps its
// recorded outcome: deciding the promoted review must not overwrite it.
func TestRecordDecision_LeavesDecidedLegacyBypassAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, "https://mcp.example.com/sse", "blocked-user", "hit the block")
	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)

	_, err = riskrepo.New(ti.conn).UpdateRiskPolicyBypassRequestStatus(ctx, riskrepo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               "denied",
		DecidedBy:            conv.ToPGText("legacy-admin"),
		GrantedPrincipalUrns: []string{},
		ID:                   bypassID,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(promoted.ID, "approved"))
	require.NoError(t, err)

	bypass, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
		ID: bypassID, ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "denied", bypass.Status)
	require.Equal(t, "legacy-admin", bypass.DecidedBy.String)
}

func TestRecordDecision_RejectsUnknownDecision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	for _, decision := range []string{"", "maybe", "APPROVED", "approve"} {
		_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), decision))
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

func TestRecordDecision_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RecordDecision(ctx, decisionPayload("not-a-uuid", "approved"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Reading the queue does not carry the right to commit the organisation to a
// server.
func TestRecordDecision_ReadScopeIsNotEnough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})
	readOnly := withProject(t, ctx, ti, ti.projectID, authz.ScopeMCPApprovalRead)

	_, err := ti.service.RecordDecision(readOnly, decisionPayload(requestID.String(), "approved"))
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// Each decision carries the evidence frozen at its own decision time, so after
// a re-gather the API can still show what an older decision actually rested
// on rather than only what the request says now.
func TestRecordDecision_DecisionsExposeTheirFrozenEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested", evidence: `{"authority":{"mode":"api_key"}}`, version: 3,
	})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)

	seedEvidence(t, ctx, ti, ti.projectID, requestID, `{"authority":{"mode":"oauth"}}`, 4)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Len(t, detail.Decisions, 1)

	// The request shows the new gather; the decision still shows the old one.
	require.NotNil(t, detail.EvidenceVersion)
	require.Equal(t, 4, *detail.EvidenceVersion)

	frozen, ok := detail.Decisions[0].Evidence.(map[string]any)
	require.True(t, ok)
	authority, ok := frozen["authority"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "api_key", authority["mode"])
	require.NotNil(t, detail.Decisions[0].EvidenceVersion)
	require.Equal(t, 3, *detail.Decisions[0].EvidenceVersion)
}

// A decision writes one audit entry in the same transaction as the decision
// itself, with the action carrying approve versus deny.
func TestRecordDecision_WritesAnAuditEntry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	approveBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestApprove)
	require.NoError(t, err)
	denyBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestDeny)
	require.NoError(t, err)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	approveAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestApprove)
	require.NoError(t, err)
	require.Equal(t, approveBefore+1, approveAfter)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)

	denyAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestDeny)
	require.NoError(t, err)
	require.Equal(t, denyBefore+1, denyAfter)
}

// Holding the scope must not bypass a disabled product feature: the grant says
// who may use the surface, the feature says whether the organization has it.
func TestRecordDecision_FeatureDisabledIsForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})
	disableMCPApproval(t, ctx, ti)

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.ListRequests(ctx, listPayload())
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.GetRequest(ctx, getPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The rationale is what gets cited when explaining the decision to the
// requester, so a blank one is rejected rather than recorded.
func TestRecordDecision_RequiresARationale(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	for _, blank := range []string{"", "   ", "\n\t"} {
		payload := decisionPayload(requestID.String(), "approved")
		payload.Rationale = blank
		_, err := ti.service.RecordDecision(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// A re-request reopens a denied review — the denial stays in the history and
// the request returns to the queue — while an approval stays until an admin
// re-decides it.
func TestUpsertApprovalRequest_ReopensDeniedOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	denied := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://denied.example.com", status: "requested", evidence: "", version: 0})
	_, err := ti.service.RecordDecision(ctx, decisionPayload(denied.String(), "denied"))
	require.NoError(t, err)

	reopened := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://denied.example.com", status: "requested", evidence: "", version: 0})
	require.Equal(t, denied, reopened, "a re-request reuses the same row")
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, denied))
	require.Len(t, decisionsFor(t, ctx, ti, ti.projectID, denied), 1, "the denial stays in the history")

	approved := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://approved.example.com", status: "requested", evidence: "", version: 0})
	_, err = ti.service.RecordDecision(ctx, decisionPayload(approved.String(), "approved"))
	require.NoError(t, err)

	seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://approved.example.com", status: "requested", evidence: "", version: 0})
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, approved), "re-asking for an approved server changes nothing")
}

// A decision can cite the research report that informed it — but only a
// report belonging to the request being decided, resolved under the caller's
// project before anything is written.
func TestRecordDecision_CitesAResearchReport(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})
	reportID := seedResearchReport(t, ctx, ti, ti.projectID, requestID, "completed", `{"claims":[]}`)

	payload := decisionPayload(requestID.String(), "approved")
	payload.ResearchReportID = new(reportID.String())

	decision, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, decision.ResearchReportID)
	require.Equal(t, reportID.String(), *decision.ResearchReportID)

	// Another request's report cannot be cited, however real it is.
	otherRequest := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})
	otherPayload := decisionPayload(otherRequest.String(), "approved")
	otherPayload.ResearchReportID = new(reportID.String())

	_, err = ti.service.RecordDecision(ctx, otherPayload)
	requireOopsCode(t, err, oops.CodeBadRequest)
}
