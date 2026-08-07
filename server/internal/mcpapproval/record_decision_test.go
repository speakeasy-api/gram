package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func decisionPayload(id, decision string) *gen.RecordDecisionPayload {
	return &gen.RecordDecisionPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		ID: id, Decision: decision, Rationale: nil, GrantedPrincipalUrns: nil,
	}
}

func TestRecordDecision_Approve(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "approved")
	payload.Rationale = new("read-only tools, pinned version, vendor we already use")
	payload.GrantedPrincipalUrns = []string{"urn:gram:team:platform"}

	decision, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, "approved", decision.Decision)
	require.NotEmpty(t, decision.DecidedBy)
	require.NotNil(t, decision.Rationale)
	require.Equal(t, []string{"urn:gram:team:platform"}, decision.GrantedPrincipalUrns)

	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// A denial grants nobody anything, whatever the caller sent alongside it.
func TestRecordDecision_DenyDropsGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "denied")
	payload.Rationale = new("demands a broad token and publishes no source")
	payload.GrantedPrincipalUrns = []string{"urn:gram:team:platform", "urn:gram:user:someone"}

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
	first.Rationale = new("no pinned version")
	_, err := ti.service.RecordDecision(ctx, first)
	require.NoError(t, err)

	second := decisionPayload(requestID.String(), "approved")
	second.Rationale = new("now pinned")
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
