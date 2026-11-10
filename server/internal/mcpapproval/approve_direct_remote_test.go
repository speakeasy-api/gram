package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An administrator's write becomes one approved inventory entry: the request
// exists with the administrator as its requester, its status is approved, and
// the decision names the audience the write reached. A later write widens the
// audience rather than replacing it.
func TestApproveDirectRemoteRecordsAnApprovedEntryAndWidensIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	// The administrator is a member of the organization they administer;
	// decisions reject granted principals that do not resolve in it.
	actor := ti.authContext.UserID
	actorURN := seedMemberPrincipal(t, ctx, ti, actor)
	const target = "https://mcp.example.test/server"

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: target,
		PrincipalURNs: []string{actorURN}, ActorUserID: actor, ActorEmail: ti.authContext.Email, Rationale: "shared with support",
	}))
	require.NoError(t, tx.Commit(ctx))

	requestID, err := ti.service.ResolveDecisionTarget(ctx, ti.organizationID, ti.projectID, "server_url", target)
	require.NoError(t, err)
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
	decisions := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, decisions, 1)
	require.Equal(t, "approved", decisions[0].Decision)
	require.Equal(t, actor, decisions[0].DecidedBy)
	require.Equal(t, []string{actorURN}, decisions[0].GrantedPrincipalUrns)
	requester, err := ti.repo.GetPlatformRequesterApprovalRequest(ctx, repo.GetPlatformRequesterApprovalRequestParams{
		ID: requestID, OrganizationID: ti.organizationID, ProjectID: ti.projectID, UserID: actor,
	})
	require.NoError(t, err, "the administrator is attached as the requester")
	require.Equal(t, target, requester.TargetRaw)

	// Sharing with everyone widens the standing approval to everyone.
	tx = testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: target,
		PrincipalURNs: []string{urn.PrincipalWildcard}, ActorUserID: actor, ActorEmail: nil, Rationale: "",
	}))
	require.NoError(t, tx.Commit(ctx))
	decisions = decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, decisions, 2)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, decisions[0].GrantedPrincipalUrns, "the newest decision is first and covers everyone")
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
	verdict, err := shadowadmission.Evaluate([]string{actorURN, "role:custom:anything"}, shadowadmission.Decision{Decision: "approved", GrantedPrincipalURNs: decisions[0].GrantedPrincipalUrns})
	require.NoError(t, err)
	require.Equal(t, shadowadmission.StateCovered, verdict.State)
}

// A previously denied server is reopened and approved by the administrator's
// write; the denial stays in history.
func TestApproveDirectRemoteOverridesAnEarlierDenial(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	actor := ti.authContext.UserID
	const target = "https://mcp.example.test/denied"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: target, status: "requested", evidence: `{}`, version: 1})
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err := ti.service.DecideInTransaction(ctx, tx, decisionInput(ti, requestID, "denied", nil))
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, "denied", requestStatus(t, ctx, ti, ti.projectID, requestID))

	tx = testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: target,
		PrincipalURNs: []string{urn.PrincipalWildcard}, ActorUserID: actor, ActorEmail: nil, Rationale: "shared with everyone",
	}))
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
	decisions := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, decisions, 2)
	require.Equal(t, "approved", decisions[0].Decision)
	require.Equal(t, "denied", decisions[1].Decision)
}

// A principal approved earlier that has since left the organization must not
// block an administrator's later write: it is dropped from the carried-forward
// audience instead of being re-submitted as new input.
func TestApproveDirectRemoteDropsAStandingPrincipalThatNoLongerResolves(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	actor := ti.authContext.UserID
	actorURN := seedMemberPrincipal(t, ctx, ti, actor)
	const target = "https://mcp.example.test/stale"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: target, status: "approved", evidence: `{}`, version: 1})
	gone := urn.NewPrincipal(urn.PrincipalTypeUser, "user-who-left").String()
	_, err := ti.repo.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, McpApprovalRequestID: requestID,
		Decision: "approved", DecidedBy: "earlier-admin", EvidenceSnapshot: []byte(`{}`), EvidenceVersion: 1, GrantedPrincipalUrns: []string{gone},
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: target,
		PrincipalURNs: []string{actorURN}, ActorUserID: actor, ActorEmail: ti.authContext.Email, Rationale: "shared with support",
	}))
	require.NoError(t, tx.Commit(ctx))
	decisions := decisionsFor(t, ctx, ti, ti.projectID, requestID)
	require.Len(t, decisions, 2)
	require.Equal(t, []string{actorURN}, decisions[0].GrantedPrincipalUrns, "the vanished principal is not carried forward")
}

// Nothing is written for an invalid ask.
func TestApproveDirectRemoteRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.Error(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: "", ActorUserID: "admin"}))
	require.Error(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: "http://insecure.example.test/mcp", ActorUserID: "admin"}), "only https servers can be approved")
	require.Error(t, ti.service.ApproveDirectRemote(ctx, tx, shadowadmission.ApproveDirectRemoteInput{OrganizationID: ti.organizationID, ProjectID: ti.projectID, CanonicalURL: "https://mcp.example.test/x", ActorUserID: ""}))
}

func decisionInput(ti *testInstance, requestID uuid.UUID, decision string, granted []string) mcpapproval.DecisionCommandInput {
	return mcpapproval.DecisionCommandInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, RequestID: requestID,
		Decision: decision, Rationale: "test decision", GrantedPrincipalURNs: granted,
		ResearchReportID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ActorUserID: ti.authContext.UserID, ActorEmail: ti.authContext.Email,
		ValidateLocked: nil,
	}
}
