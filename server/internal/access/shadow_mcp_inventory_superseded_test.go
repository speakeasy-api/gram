package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// seedApprovalDecisionRow plants a decision on a request the way the record
// path writes them, so tests can model reviews whose decision history exists
// independent of the request's current status.
func seedApprovalDecisionRow(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, projectID uuid.UUID, requestID uuid.UUID, decision string, principals []string) {
	t.Helper()

	_, err := mcpapprovalrepo.New(ti.conn).CreateApprovalDecision(ctx, mcpapprovalrepo.CreateApprovalDecisionParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		McpApprovalRequestID: requestID,
		Decision:             decision,
		DecidedBy:            "user-reviewer",
		Rationale:            conv.ToPGText("decided in a test"),
		EvidenceSnapshot:     []byte(`{}`),
		EvidenceVersion:      1,
		GrantedPrincipalUrns: principals,
		McpResearchReportID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
}

// A superseded review carries no verdict: the row keeps its status and its
// decision history, but the access summary reports pure mechanism state —
// none of the "Approved by review, but…" contradiction family — because an
// admin explicitly displaced the decision. The drifted control row proves the
// difference: same grant state, but its still-approved review surfaces the
// contradiction.
func TestService_ShadowMCPInventory_SupersededDecisionCarriesNoVerdict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	organizationID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, organizationID)})

	createShadowMCPInventoryPolicy(t, ctx, ti, shadowMCPInventoryPolicyInput{
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		Name:           "Superseded Verdict Policy",
		Action:         "block",
		Disposition:    "block_all",
	})

	supersededURL := "https://superseded.example.com/mcp"
	driftedURL := "https://drifted.example.com/mcp"
	allUsers := []string{authz.AllUsersPrincipal().String()}

	// Both reviews were approved and both approvals' grants are gone. The
	// difference is intent: one approval was explicitly superseded by a
	// policy edit, the other silently drifted. The third review was denied
	// and then reopened by a fresh ask — its denial still stands.
	reopenedURL := "https://reopened.example.com/mcp"
	superseded := seedShadowMCPApprovalRequest(t, ctx, ti, organizationID, projectID, supersededURL, "superseded", 0)
	seedApprovalDecisionRow(t, ctx, ti, organizationID, projectID, superseded.ID, "approved", allUsers)
	drifted := seedShadowMCPApprovalRequest(t, ctx, ti, organizationID, projectID, driftedURL, "approved", 0)
	seedApprovalDecisionRow(t, ctx, ti, organizationID, projectID, drifted.ID, "approved", allUsers)
	reopened := seedShadowMCPApprovalRequest(t, ctx, ti, organizationID, projectID, reopenedURL, "requested", 1)
	seedApprovalDecisionRow(t, ctx, ti, organizationID, projectID, reopened.ID, "denied", []string{})

	result, err := ti.service.ListShadowMCPInventory(ctx, &gen.ListShadowMCPInventoryPayload{
		ProjectID: projectID.String(),
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 3)

	byURL := make(map[string]*gen.ShadowMCPInventoryServer, len(result.Servers))
	for _, server := range result.Servers {
		byURL[server.CanonicalServerURL] = server
	}

	supersededRow := byURL[supersededURL]
	require.NotNil(t, supersededRow.ApprovalRequest)
	require.Equal(t, "superseded", supersededRow.ApprovalRequest.Status)
	require.Nil(t, supersededRow.ApprovalRequest.StandingDecision)
	require.NotNil(t, supersededRow.AccessSummary)
	require.Equal(t, shadowMCPAccessStateBlocked, supersededRow.AccessSummary.State)
	require.Nil(t, supersededRow.AccessSummary.Decision)
	require.Equal(t, shadowMCPAccessCoverageNone, supersededRow.AccessSummary.DecisionCoverage)

	driftedRow := byURL[driftedURL]
	require.NotNil(t, driftedRow.AccessSummary)
	require.Equal(t, shadowMCPAccessStateBlocked, driftedRow.AccessSummary.State)
	require.NotNil(t, driftedRow.AccessSummary.Decision)
	require.Equal(t, "approved", *driftedRow.AccessSummary.Decision)
	require.Equal(t, shadowMCPAccessCoveragePartial, driftedRow.AccessSummary.DecisionCoverage)
	require.NotNil(t, driftedRow.ApprovalRequest.StandingDecision)
	require.Equal(t, "approved", *driftedRow.ApprovalRequest.StandingDecision)

	// The reopened review's lifecycle reads requested, but its denial is
	// still the standing intent — clients checking edits against standing
	// decisions read this field, not the status.
	reopenedRow := byURL[reopenedURL]
	require.NotNil(t, reopenedRow.ApprovalRequest)
	require.Equal(t, "requested", reopenedRow.ApprovalRequest.Status)
	require.NotNil(t, reopenedRow.ApprovalRequest.StandingDecision)
	require.Equal(t, "denied", *reopenedRow.ApprovalRequest.StandingDecision)
}

// The server detail page reports the superseded review the same way the
// list does — status passthrough, no verdict.
func TestService_GetShadowMCPInventoryServer_SupersededStatusPassesThrough(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx := testAccessAuthContext(t, ctx)
	organizationID := authCtx.ActiveOrganizationID
	projectID := *authCtx.ProjectID
	ctx = withRBACGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, organizationID)})

	serverURL := "https://superseded-detail.example.com/mcp"
	request := seedShadowMCPApprovalRequest(t, ctx, ti, organizationID, projectID, serverURL, "superseded", 1)
	seedApprovalDecisionRow(t, ctx, ti, organizationID, projectID, request.ID, "denied", []string{})

	server, err := ti.service.GetShadowMCPInventoryServer(ctx, &gen.GetShadowMCPInventoryServerPayload{
		ProjectID:  projectID.String(),
		ServerSlug: shadowmcp.ServerSlug(serverURL),
	})
	require.NoError(t, err)

	require.NotNil(t, server.ApprovalRequest)
	require.Equal(t, "superseded", server.ApprovalRequest.Status)
	require.NotNil(t, server.AccessSummary)
	require.Nil(t, server.AccessSummary.Decision)
}
