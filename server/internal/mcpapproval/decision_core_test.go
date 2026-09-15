package mcpapproval_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

//nolint:glint // this test intentionally owns a transaction to prove the shared callback runs before every decision write.
func TestDecideInTransactionValidatesLockedStateBeforeWriting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://version.example.test", status: "requested", evidence: `{}`, version: 1})
	before, err := ti.repo.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	conflict := errors.New("stale decision version")

	tx, err := ti.conn.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = ti.service.DecideInTransaction(ctx, tx, mcpapproval.DecisionCommandInput{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, RequestID: requestID,
		Decision: "denied", Rationale: "review changed", GrantedPrincipalURNs: []string{},
		ResearchReportID: uuid.NullUUID{}, ActorUserID: ti.authContext.UserID, ActorEmail: ti.authContext.Email,
		ValidateLocked: func(state mcpapproval.DecisionVersionState) error {
			require.Equal(t, requestID, state.RequestID)
			require.Equal(t, "requested", state.Status)
			return conflict
		},
	})
	require.ErrorIs(t, err, conflict)
	require.NoError(t, tx.Rollback(ctx))

	after, err := ti.repo.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}
