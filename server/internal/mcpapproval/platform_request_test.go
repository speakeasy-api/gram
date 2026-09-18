package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestReadPlatformRequesterReviewIsOwnerBoundAndPrivacyMinimized(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreatePlatformRequest(ctx, ti.organizationID, ti.projectID, ti.authContext.UserID, "server_url", "https://mcp.example.com/sse?token=fabricated", "private justification")
	require.NoError(t, err)
	requestID := uuid.MustParse(created.ID)

	result, err := ti.service.ReadPlatformRequesterReview(ctx, mcpapproval.PlatformRequesterReviewInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, UserID: ti.authContext.UserID, RequestID: requestID,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, result.RequestID)
	require.Equal(t, "requested", result.Status)
	require.Equal(t, "wait_for_review", result.NextAction)
	require.NotContains(t, result.Target, "fabricated")
	require.NotEmpty(t, result.RequestedAt)

	_, err = ti.service.ReadPlatformRequesterReview(ctx, mcpapproval.PlatformRequesterReviewInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, UserID: "another-user", RequestID: requestID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	_, err = ti.service.ReadPlatformRequesterReview(ctx, mcpapproval.PlatformRequesterReviewInput{
		OrganizationID: ti.organizationID, ProjectID: otherProject, UserID: ti.authContext.UserID, RequestID: requestID,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestPlatformRequesterNextActionPrioritizesPendingStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreatePlatformRequest(ctx, ti.organizationID, ti.projectID, ti.authContext.UserID, "server_url", "https://pending.example.test/mcp", "needed for work")
	require.NoError(t, err)
	requestID := uuid.MustParse(created.ID)
	_, err = ti.repo.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, McpApprovalRequestID: requestID,
		Decision: "denied", DecidedBy: "admin", EvidenceSnapshot: []byte(`{}`), EvidenceVersion: 1, GrantedPrincipalUrns: []string{},
	})
	require.NoError(t, err)
	_, err = ti.service.CreatePlatformRequest(ctx, ti.organizationID, ti.projectID, ti.authContext.UserID, "server_url", "https://pending.example.test/mcp", "still needed")
	require.NoError(t, err)

	result, err := ti.service.ReadPlatformRequesterReview(ctx, mcpapproval.PlatformRequesterReviewInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, UserID: ti.authContext.UserID, RequestID: requestID,
	})
	require.NoError(t, err)
	require.Equal(t, "requested", result.Status)
	require.Equal(t, "denied", result.StandingDecision)
	require.Equal(t, "wait_for_review", result.NextAction)
}

func TestCreatePlatformRequestUsesExistingAdmissionAndRolloutGate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created, err := ti.service.CreatePlatformRequest(ctx, ti.organizationID, ti.projectID, ti.authContext.UserID, "stdio_command", "FAKE_TOKEN=fabricated npx -y example-mcp", "needed for work")
	require.NoError(t, err)
	require.Equal(t, "requested", created.Status)
	require.NotContains(t, created.TargetRaw, "fabricated")
	require.Equal(t, 1, created.RequesterCount)

	disableMCPApproval(ti)
	_, err = ti.service.CreatePlatformRequest(ctx, ti.organizationID, ti.projectID, ti.authContext.UserID, "server_url", "https://other.example.test/mcp", "needed for work")
	requireOopsCode(t, err, oops.CodeForbidden)
}
